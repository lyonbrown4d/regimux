package auth

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/arcgolabs/authx"
	authxhttp "github.com/arcgolabs/authx/http"
	authjwt "github.com/arcgolabs/authx/jwt"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/samber/oops"
)

type Service struct {
	cfg      config.Config
	auth     config.RegistryAuthConfig
	logger   *slog.Logger
	engine   *authx.Engine
	tokenTTL time.Duration
	secret   []byte
}

func NewService(cfg config.Config, logger *slog.Logger) (*Service, error) {
	if logger == nil {
		logger = slog.Default()
	}
	service := &Service{
		cfg:      cfg,
		auth:     cfg.Auth,
		logger:   logger.With("component", "auth"),
		tokenTTL: cfg.Auth.TokenTTL,
		secret:   []byte(strings.TrimSpace(cfg.Auth.TokenSecret)),
	}
	if service.tokenTTL <= 0 {
		service.tokenTTL = 15 * time.Minute
	}
	if !service.Enabled() {
		service.logger.Info("registry auth disabled")
		return service, nil
	}

	manager := authx.NewProviderManager(
		authx.NewAuthenticationProviderFunc(service.authenticateBasic),
		authjwt.NewAuthenticationProvider(
			authjwt.WithHMACSecret(service.secret),
			authjwt.WithIssuer(service.issuer()),
			authjwt.WithAudience(service.serviceName()),
			authjwt.WithRequiredExpiration(),
			authjwt.WithRequiredIssuedAt(),
			authjwt.WithClockSkew(time.Minute),
		),
	)
	service.engine = authx.NewEngine(
		authx.WithAuthenticationManager(manager),
		authx.WithAuthorizer(authx.AuthorizerFunc(service.authorize)),
		authx.WithLogger(logger),
	)
	service.logger.Info("registry auth enabled", "users", len(cfg.Auth.Users), "service", service.serviceName(), "issuer", service.issuer(), "token_ttl", service.tokenTTL)
	return service, nil
}

func (s *Service) Enabled() bool {
	return s != nil && s.auth.Enabled
}

func (s *Service) IssueToken(ctx context.Context, req TokenRequest) (TokenResponse, error) {
	if !s.Enabled() || s.engine == nil {
		return TokenResponse{}, oops.In("auth").Errorf("registry auth is not enabled")
	}
	if err := s.validateTokenService(req.Service); err != nil {
		return TokenResponse{}, err
	}
	principal, err := s.authenticateTokenUser(ctx, req)
	if err != nil {
		return TokenResponse{}, err
	}
	scopes := normalizeScopes(req.Scopes)
	if scopeErr := s.validateRequestedScopes(scopes, principal.ID); scopeErr != nil {
		return TokenResponse{}, scopeErr
	}
	token, err := s.signToken(principal, scopes)
	if err != nil {
		return TokenResponse{}, err
	}
	s.logger.DebugContext(ctx, "registry token issued", "subject", principal.ID, "scopes", len(scopes), "expires_in", token.ExpiresIn)
	return token, nil
}

// AuthenticateBasic validates configured registry credentials and returns the
// authenticated principal.
func (s *Service) AuthenticateBasic(ctx context.Context, username, password string) (authx.Principal, error) {
	if !s.Enabled() || s.engine == nil {
		return authx.Principal{}, oops.In("auth").Errorf("registry auth is not enabled")
	}
	result, err := s.engine.Check(ctx, BasicCredential{
		Username: username,
		Password: password,
	})
	if err != nil {
		s.logger.DebugContext(ctx, "basic authentication failed", "username", username, "error", err)
		return authx.Principal{}, wrapAuthError(err, authx.ErrorCodeUnauthenticated, "check basic credential")
	}
	principal, ok := authx.PrincipalFromAny(result.Principal)
	if !ok {
		return authx.Principal{}, newAuthError(authx.ErrorCodeUnauthenticated, "basic credential did not produce a principal")
	}
	s.logger.DebugContext(ctx, "basic authentication succeeded", "username", principal.ID)
	return principal, nil
}

func (s *Service) signToken(principal authx.Principal, scopes []string) (TokenResponse, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.tokenTTL)
	claims := authjwt.Claims{
		Roles:       principalListValues(principal.Roles),
		Permissions: scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   principal.ID,
			Issuer:    s.issuer(),
			Audience:  jwt.ClaimStrings{s.serviceName()},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return TokenResponse{}, oops.In("auth").Wrapf(err, "sign registry token")
	}

	return TokenResponse{
		Token:       signed,
		AccessToken: signed,
		ExpiresIn:   int64(s.tokenTTL.Seconds()),
		IssuedAt:    now,
	}, nil
}

func (s *Service) validateTokenService(service string) error {
	serviceName := strings.TrimSpace(service)
	if serviceName == "" {
		serviceName = s.serviceName()
	}
	if serviceName == s.serviceName() {
		return nil
	}
	return newAuthError(
		authx.ErrorCodeInvalidAuthenticationCredential,
		"token service is invalid",
		"service", serviceName,
		"expected_service", s.serviceName(),
	)
}

func (s *Service) authenticateTokenUser(ctx context.Context, req TokenRequest) (authx.Principal, error) {
	return s.AuthenticateBasic(ctx, req.Username, req.Password)
}

func (s *Service) validateRequestedScopes(scopes []string, username string) error {
	var validateErr error
	collectionlist.NewList(scopes...).Range(func(_ int, scopeText string) bool {
		scope, err := parseScope(scopeText)
		if err != nil {
			validateErr = err
			return false
		}
		if scope.RequiresPull() && !s.userAllows(username, scope.Name) {
			validateErr = newHTTPAuthError(
				authxhttp.ErrorCodeAccessDenied,
				"repository access denied",
				"resource", scope.Name,
				"reason", "repository_not_allowed",
			)
			return false
		}
		return true
	})
	return validateErr
}

func (s *Service) authenticateBasic(_ context.Context, credential BasicCredential) (authx.AuthenticationResult, error) {
	username := strings.TrimSpace(credential.Username)
	if username == "" || credential.Password == "" {
		return authx.AuthenticationResult{}, newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are required")
	}
	user, ok := s.auth.Users[username]
	if !ok {
		return authx.AuthenticationResult{}, newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are invalid")
	}
	if err := verifyPassword(user, credential.Password); err != nil {
		return authx.AuthenticationResult{}, err
	}
	return authx.AuthenticationResult{
		Principal: authx.Principal{
			ID:          username,
			Roles:       collectionlist.NewListWithCapacity[string](len(user.Groups), user.Groups...),
			Permissions: collectionlist.NewListWithCapacity[string](len(user.Repositories), user.Repositories...),
		},
	}, nil
}

func (s *Service) authorize(_ context.Context, input authx.AuthorizationModel) (authx.Decision, error) {
	if input.Action == ActionRegistryPing {
		return authx.Decision{Allowed: true, PolicyID: "regimux.registry.ping"}, nil
	}
	if input.Action != ActionPull {
		return authx.Decision{Allowed: false, Reason: "unsupported_action", PolicyID: "regimux.pull"}, nil
	}
	principal, ok := authx.PrincipalFromAny(input.Principal)
	if !ok || strings.TrimSpace(principal.ID) == "" {
		return authx.Decision{Allowed: false, Reason: "invalid_principal", PolicyID: "regimux.pull"}, nil
	}
	resource := strings.Trim(input.Resource, "/")
	if resource == "" {
		return authx.Decision{Allowed: false, Reason: "missing_resource", PolicyID: "regimux.pull"}, nil
	}
	if !principalHasPullScope(principal, resource) {
		return authx.Decision{Allowed: false, Reason: "token_scope_required", PolicyID: "regimux.pull"}, nil
	}
	if !s.userAllows(principal.ID, resource) {
		return authx.Decision{Allowed: false, Reason: "repository_not_allowed", PolicyID: "regimux.pull"}, nil
	}
	return authx.Decision{Allowed: true, PolicyID: "regimux.pull"}, nil
}

func (s *Service) userAllows(username, resource string) bool {
	user, ok := s.auth.Users[username]
	if !ok {
		return false
	}
	return collectionlist.NewList(user.Repositories...).AnyMatch(func(_ int, pattern string) bool {
		return repositoryPatternMatches(pattern, resource)
	})
}

func (s *Service) AuthorizationForPath(path string, principal any) (authx.AuthorizationModel, error) {
	if isRegistryPingPath(path) {
		return authx.AuthorizationModel{
			Principal: principal,
			Action:    ActionRegistryPing,
			Resource:  "registry",
		}, nil
	}
	resource, err := s.ResourceFromPath(path)
	if err != nil {
		return authx.AuthorizationModel{}, err
	}
	return authx.AuthorizationModel{
		Principal: principal,
		Action:    ActionPull,
		Resource:  resource,
	}, nil
}

func (s *Service) ResourceFromPath(path string) (string, error) {
	route, err := reference.Parse(path)
	if err != nil {
		return "", oops.Wrapf(err, "parse registry auth resource")
	}
	if route.Repo == "" {
		return "", oops.In("auth").With("path", path).Errorf("registry auth resource is missing repository")
	}
	repo := route.Repo
	if upstreamCfg, ok := s.cfg.Upstreams[route.Alias]; ok && upstreamCfg.DefaultNamespace != "" {
		repo = route.WithDefaultNamespace(upstreamCfg.DefaultNamespace).Repo
	}
	return route.Alias + "/" + strings.Trim(repo, "/"), nil
}

func (s *Service) ScopeForPath(path string) string {
	if isRegistryPingPath(path) {
		return ""
	}
	resource, err := s.ResourceFromPath(path)
	if err != nil || resource == "" {
		return ""
	}
	return ScopeTypeRepository + ":" + resource + ":" + ActionPull
}

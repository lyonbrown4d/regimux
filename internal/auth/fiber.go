package auth

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/arcgolabs/authx"
	authxhttp "github.com/arcgolabs/authx/http"
	authfiber "github.com/arcgolabs/authx/http/fiber"
	authjwt "github.com/arcgolabs/authx/jwt"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

func (s *Service) RegisterFiber(app *fiber.App) {
	if !s.Enabled() || app == nil {
		return
	}
	app.Get("/auth/token", s.handleToken)
	app.Use("/v2", authfiber.RequireFast(s.guard(), authfiber.WithErrorResponseHandler(s.writeAuthFailure)))
}

func (s *Service) Guard() *authxhttp.Guard {
	return s.guard()
}

func (s *Service) guard() *authxhttp.Guard {
	return authxhttp.NewGuard(
		s.engine,
		authxhttp.WithCredentialResolverFunc(s.resolveCredential),
		authxhttp.WithAuthorizationResolverFunc(func(_ context.Context, req authxhttp.RequestInfo, principal any) (authx.AuthorizationModel, error) {
			return s.AuthorizationForPath(req.Path, principal)
		}),
	)
}

func (s *Service) resolveCredential(_ context.Context, req authxhttp.RequestInfo) (any, error) {
	header := strings.TrimSpace(req.Header(distribution.HeaderAuthorization))
	if header == "" {
		return authjwt.TokenCredential{}, newHTTPAuthError(authx.ErrorCodeInvalidAuthenticationCredential, "authorization bearer token is required")
	}
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(strings.TrimSpace(scheme), distribution.AuthSchemeBearer) || strings.TrimSpace(token) == "" {
		return authjwt.TokenCredential{}, newHTTPAuthError(authx.ErrorCodeInvalidAuthenticationCredential, "authorization bearer token is invalid")
	}
	return authjwt.NewTokenCredential(token), nil
}

func (s *Service) handleToken(c fiber.Ctx) error {
	username, password, ok := basicAuthFromHeader(c.Get(distribution.HeaderAuthorization))
	if !ok {
		return s.writeUnauthorized(c)
	}
	response, err := s.IssueToken(c.Context(), TokenRequest{
		Username: username,
		Password: password,
		Service:  c.Query("service"),
		Scopes:   queryScopes(c),
	})
	if err != nil {
		status := authxhttp.StatusCodeFromError(err)
		if status == http.StatusForbidden {
			return writeDistributionFailure(c, status, distribution.ErrDenied.WithDetail(nil), "")
		}
		return s.writeUnauthorized(c)
	}
	c.Set(distribution.HeaderContentType, distribution.MediaTypeJSON)
	if err := c.Status(http.StatusOK).JSON(response); err != nil {
		return oops.Wrapf(err, "write registry token response")
	}
	return nil
}

func (s *Service) writeAuthFailure(c fiber.Ctx, response authxhttp.ErrorResponse) error {
	status := response.Status
	if status == 0 {
		status = http.StatusUnauthorized
	}
	switch status {
	case http.StatusUnauthorized:
		return s.writeUnauthorized(c)
	case http.StatusForbidden:
		return writeDistributionFailure(c, http.StatusForbidden, distribution.ErrDenied.WithDetail(nil), "")
	default:
		return writeDistributionFailure(c, http.StatusInternalServerError, distribution.ErrUnknown.WithDetail(nil), "")
	}
}

func (s *Service) writeUnauthorized(c fiber.Ctx) error {
	return writeDistributionFailure(c, http.StatusUnauthorized, distribution.ErrUnauthorized.WithDetail(nil), s.challenge(c))
}

func writeDistributionFailure(c fiber.Ctx, status int, list *distribution.ErrorList, challenge string) error {
	if challenge != "" {
		c.Set(distribution.HeaderWWWAuthenticate, challenge)
	}
	c.Set(distribution.HeaderContentType, distribution.MediaTypeJSON)
	c.Set(distribution.HeaderDockerDistributionAPIVersion, distribution.APIVersion)
	if list == nil {
		list = distribution.ErrUnknown.WithDetail(nil)
	}
	list.Status = status
	body, err := distribution.MarshalError(list)
	if err != nil {
		body = []byte(`{"errors":[{"code":"UNKNOWN","message":"unknown error"}]}`)
	}
	if err := c.Status(status).Send(body); err != nil {
		return oops.Wrapf(err, "write registry auth failure response")
	}
	return nil
}

func (s *Service) challenge(c fiber.Ctx) string {
	parts := collectionlist.NewList(
		"Bearer realm="+strconv.Quote(s.realm(c)),
		"service="+strconv.Quote(s.serviceName()),
	)
	if scope := s.ScopeForPath(c.Path()); scope != "" {
		parts.Add("scope=" + strconv.Quote(scope))
	}
	return parts.Join(",")
}

func (s *Service) realm(c fiber.Ctx) string {
	if realm := strings.TrimSpace(s.auth.Realm); realm != "" {
		return realm
	}
	base := strings.TrimRight(strings.TrimSpace(s.cfg.Server.PublicURL), "/")
	if base == "" && c != nil {
		scheme := c.Protocol()
		host := c.Hostname()
		if scheme != "" && host != "" {
			base = scheme + "://" + host
		}
	}
	if base == "" {
		base = "http://localhost:8080"
	}
	return base + "/auth/token"
}

func basicAuthFromHeader(header string) (string, string, bool) {
	scheme, payload, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Basic") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return "", "", false
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" {
		return "", "", false
	}
	return username, password, true
}

func queryScopes(c fiber.Ctx) []string {
	if c == nil {
		return nil
	}
	values, err := url.ParseQuery(string(c.Request().URI().QueryString()))
	if err != nil {
		return nil
	}
	return values["scope"]
}

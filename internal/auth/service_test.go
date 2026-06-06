package auth_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/arcgolabs/authx"
	authxhttp "github.com/arcgolabs/authx/http"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/config"
	containerauth "github.com/lyonbrown4d/regimux/internal/ecosystems/container/auth"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServiceIssuesTokenAndAuthorizesPull(t *testing.T) {
	service := newTestService(t)

	token := issueTestToken(t, service, "repository:hub/library/alpine:pull")
	result, decision, err := service.Guard().Require(context.Background(), authxhttp.RequestInfo{
		Method: http.MethodGet,
		Path:   "/v2/hub/library/alpine/manifests/latest",
		Headers: http.Header{
			distribution.HeaderAuthorization: {distribution.AuthSchemeBearer + " " + token},
		},
	})
	if err != nil {
		t.Fatalf("require auth: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected allowed decision, got %#v", decision)
	}
	principal, ok := authx.PrincipalFromAny(result.Principal)
	if !ok || principal.ID != "alice" {
		t.Fatalf("principal = %#v, want alice", result.Principal)
	}
}

func TestServiceDeniesUnauthorizedTokenScope(t *testing.T) {
	service := newTestService(t)

	_, err := service.IssueToken(context.Background(), auth.TokenRequest{
		Username: "alice",
		Password: "secret",
		Service:  "regimux",
		Scopes:   []string{"repository:hub/library/redis:pull"},
	})
	if err == nil {
		t.Fatal("expected token issuance to fail")
	}
	if got := authxhttp.StatusCodeFromError(err); got != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", got)
	}
}

func TestServiceDeniesPullWithUnscopedToken(t *testing.T) {
	service := newTestService(t)

	response, err := service.IssueToken(context.Background(), auth.TokenRequest{
		Username: "alice",
		Password: "secret",
		Service:  "regimux",
	})
	if err != nil {
		t.Fatalf("issue unscoped token: %v", err)
	}
	_, decision, err := service.Guard().Require(context.Background(), authxhttp.RequestInfo{
		Method: http.MethodGet,
		Path:   "/v2/hub/library/alpine/manifests/latest",
		Headers: http.Header{
			distribution.HeaderAuthorization: {distribution.AuthSchemeBearer + " " + response.Token},
		},
	})
	if err != nil {
		t.Fatalf("require auth: %v", err)
	}
	if decision.Allowed || decision.Reason != "token_scope_required" {
		t.Fatalf("decision = %#v, want token_scope_required denial", decision)
	}
}

func TestServiceAuthenticatesBasicCredential(t *testing.T) {
	service := newTestService(t)

	principal, err := service.AuthenticateBasic(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("authenticate basic: %v", err)
	}
	if principal.ID != "alice" {
		t.Fatalf("principal ID = %q, want alice", principal.ID)
	}
}

func TestServiceChallengeUsesDefaultNamespaceScope(t *testing.T) {
	service := newTestService(t)

	got := service.ScopeForPath("/v2/hub/alpine/manifests/latest")
	want := "repository:hub/library/alpine:pull"
	if got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
}

func issueTestToken(t *testing.T, service *auth.Service, scope string) string {
	t.Helper()

	response, err := service.IssueToken(context.Background(), auth.TokenRequest{
		Username: "alice",
		Password: "secret",
		Service:  "regimux",
		Scopes:   []string{scope},
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if response.Token == "" || response.Token != response.AccessToken {
		t.Fatalf("unexpected token response %#v", response)
	}
	return response.Token
}

func newTestService(t *testing.T) *auth.Service {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Server.PublicURL = "http://regimux.test"
	cfg.Auth = config.RegistryAuthConfig{
		Enabled:     true,
		Service:     "regimux",
		Issuer:      "regimux",
		TokenSecret: "test-secret",
		Users: map[string]config.AuthUserConfig{
			"alice": {
				Password:     "secret",
				Repositories: []string{"hub/library/alpine", "ghcr/org/*"},
				Groups:       []string{"developers"},
			},
		},
	}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	resolvers := collectionlist.NewList[auth.ResourceResolver]()
	resolvers.Add(containerauth.NewResourceResolver(cfg))

	service, err := auth.NewService(cfg, nil, resolvers)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service
}

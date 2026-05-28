package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServerAuthenticatesRegistryPullWithBearerToken(t *testing.T) {
	authService := newTestAuthService(t)
	baseURL := startAPIServerWithOptions(t, api.Options{
		Auth: authService,
		Endpoints: []httpx.Endpoint{
			api.NewRegistryEndpointFromConfig(
				&recordingManifestService{},
				nil,
				nil,
				nil,
				nil,
				authTestConfig(),
			),
		},
	})

	unauthenticated := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	readHTTPResponse(t, unauthenticated)
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", unauthenticated.StatusCode)
	}
	challenge := unauthenticated.Header.Get(distribution.HeaderWWWAuthenticate)
	if !strings.Contains(challenge, `Bearer realm="http://regimux.test/auth/token"`) {
		t.Fatalf("challenge = %q, want bearer realm", challenge)
	}
	if !strings.Contains(challenge, `scope="repository:hub/library/alpine:pull"`) {
		t.Fatalf("challenge = %q, want repository scope", challenge)
	}

	token := requestAuthToken(t, baseURL, "repository:hub/library/alpine:pull")
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/v2/hub/library/alpine/manifests/latest", http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(distribution.HeaderAuthorization, distribution.AuthSchemeBearer+" "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
}

func requestAuthToken(t *testing.T, baseURL, scope string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/auth/token?service=regimux&scope="+scope, http.NoBody)
	if err != nil {
		t.Fatalf("build token request: %v", err)
	}
	req.SetBasicAuth("alice", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send token request: %v", err)
	}
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d body=%q, want 200", resp.StatusCode, body)
	}
	var token auth.TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		t.Fatalf("decode token response: %v body=%q", err, body)
	}
	if token.Token == "" {
		t.Fatalf("token response missing token: %#v", token)
	}
	return token.Token
}

func newTestAuthService(t *testing.T) *auth.Service {
	t.Helper()

	service, err := auth.NewService(authTestConfig(), nil)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return service
}

func authTestConfig() config.Config {
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
				Repositories: []string{"hub/library/alpine"},
			},
		},
	}
	return cfg
}

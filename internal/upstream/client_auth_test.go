package upstream_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientGetManifestBearerChallenge(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	authServer := httptest.NewServer(challengeTokenHandler(t, &tokenRequests))
	defer authServer.Close()

	var manifestRequests atomic.Int32
	registryServer := httptest.NewServer(challengeManifestHandler(t, authServer.URL, &manifestRequests))
	defer registryServer.Close()

	client := upstream.NewClient(map[string]upstream.Config{
		"hub": {
			Registry: registryServer.URL,
			Auth: upstream.AuthConfig{
				Type:     "dockerhub",
				Username: "user",
				Password: "pass",
			},
		},
	}, nil)

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
		Accept:        distribution.MediaTypeDockerManifest,
	})
	requireNoError(t, err, "GetManifest")
	requireEqual(t, readAndClose(t, resp.Body), `{"schemaVersion":2}`, "manifest body")
	requireEqual(t, resp.Digest, "sha256:abc", "manifest digest")
	requireEqual(t, tokenRequests.Load(), int32(1), "token requests")
	requireEqual(t, manifestRequests.Load(), int32(2), "manifest requests")
}

func TestClientGetManifestCachesBearerTokenForSameScope(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	authServer := httptest.NewServer(cachedTokenHandler(t, &tokenRequests))
	defer authServer.Close()

	registryServer := httptest.NewServer(cachedManifestHandler(t, authServer.URL))
	defer registryServer.Close()

	client := upstream.NewClient(map[string]upstream.Config{
		"hub": {Registry: registryServer.URL},
	}, nil)

	for i := range 2 {
		resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
			UpstreamAlias: "hub",
			Repo:          "library/nginx",
			Reference:     "latest",
		})
		requireNoError(t, err, "GetManifest #"+strconv.Itoa(i+1))
		closeBody(t, resp.Body)
	}

	requireEqual(t, tokenRequests.Load(), int32(1), "token requests")
}

func TestClientGetManifestDoesNotShareBearerTokenAcrossScopes(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	authServer := httptest.NewServer(scopedTokenHandler(t, &tokenRequests))
	defer authServer.Close()

	registryServer := httptest.NewServer(scopedManifestHandler(t, authServer.URL))
	defer registryServer.Close()

	client := upstream.NewClient(map[string]upstream.Config{
		"hub": {Registry: registryServer.URL},
	}, nil)

	for _, repo := range []string{"library/nginx", "library/redis"} {
		resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
			UpstreamAlias: "hub",
			Repo:          repo,
			Reference:     "latest",
		})
		requireNoError(t, err, "GetManifest "+repo)
		closeBody(t, resp.Body)
	}

	requireEqual(t, tokenRequests.Load(), int32(2), "token requests")
}

func challengeTokenHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Path, "/token", "token path")
		requireEqual(t, r.URL.Query().Get("service"), "registry.test", "token service")
		requireEqual(t, r.URL.Query().Get("scope"), "repository:library/nginx:pull", "token scope")

		username, password, ok := r.BasicAuth()
		requireEqual(t, ok, true, "token basic auth presence")
		requireEqual(t, username, "user", "token basic auth username")
		requireEqual(t, password, "pass", "token basic auth password")
		writeJSON(t, w, map[string]string{"token": "challenge-token"})
	}
}

func challengeManifestHandler(t *testing.T, authURL string, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/library/nginx/manifests/latest", "manifest path")
		requireEqual(t, r.Header.Get("Accept"), distribution.MediaTypeDockerManifest, "manifest accept")
		if requests.Load() == 1 {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, authURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		requireEqual(t, r.Header.Get("Authorization"), "Bearer challenge-token", "manifest authorization")
		body := `{"schemaVersion":2}`
		w.Header().Set("Docker-Content-Digest", "sha256:abc")
		w.Header().Set("Content-Type", distribution.MediaTypeDockerManifest)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		writeString(t, w, body)
	}
}

func cachedTokenHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Query().Get("scope"), "repository:library/nginx:pull", "token scope")
		writeJSON(t, w, map[string]any{
			"token":      "cached-token",
			"expires_in": 3600,
			"issued_at":  time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
}

func cachedManifestHandler(t *testing.T, authURL string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requireEqual(t, r.URL.Path, "/v2/library/nginx/manifests/latest", "manifest path")
		got := r.Header.Get("Authorization")
		if got == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, authURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		requireEqual(t, got, "Bearer cached-token", "manifest authorization")
		writeString(t, w, `{"schemaVersion":2}`)
	}
}

func scopedTokenHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		token := tokenForScope(t, r.URL.Query().Get("scope"))
		writeJSON(t, w, map[string]any{
			"token":      token,
			"expires_in": 3600,
			"issued_at":  time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
}

func tokenForScope(t *testing.T, scope string) string {
	t.Helper()
	switch scope {
	case "repository:library/nginx:pull":
		return "nginx-token"
	case "repository:library/redis:pull":
		return "redis-token"
	default:
		t.Fatalf("token scope = %q, want nginx or redis repository scope", scope)
		return ""
	}
}

func scopedManifestHandler(t *testing.T, authURL string) http.HandlerFunc {
	t.Helper()
	expectedAuth := map[string]string{
		"/v2/library/nginx/manifests/latest": "Bearer nginx-token",
		"/v2/library/redis/manifests/latest": "Bearer redis-token",
	}
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		wantAuth, ok := expectedAuth[r.URL.Path]
		requireEqual(t, ok, true, "known manifest path")
		got := r.Header.Get("Authorization")
		if got == "" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, authURL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		requireEqual(t, got, wantAuth, "manifest authorization")
		writeString(t, w, `{"schemaVersion":2}`)
	}
}

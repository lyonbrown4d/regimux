package registrytool_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/registrytool"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestClientFetchManifestAcceptsSameHostPrivateBearerRealm(t *testing.T) {
	body := []byte(`{"schemaVersion":2}`)
	digest := ocidigest.FromBytes(body).String()
	var tokenRequests atomic.Int64
	var challengedRequests atomic.Int64
	var authorizedRequests atomic.Int64
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests.Add(1)
		if r.URL.Path != "/token" || r.URL.Query().Get("service") != "registry.test" ||
			!strings.Contains(r.URL.Query().Get("scope"), "repository:library/node:pull") {
			http.Error(w, "unexpected token request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeAuthTestBody(t, w, []byte(`{"token":"registry-token"}`))
	}))
	defer tokenServer.Close()

	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/library/node/manifests/latest" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer registry-token" {
			challengedRequests.Add(1)
			w.Header().Set("WWW-Authenticate", bearerChallenge(tokenServer.URL+"/token"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authorizedRequests.Add(1)
		writeAuthTestManifest(t, w, r, body, digest)
	}))
	defer registryServer.Close()

	manifest, err := registrytool.NewClient().FetchManifest(t.Context(), registrytool.Reference{
		Registry: registryServer.URL, Repository: "library/node", Reference: "latest",
	})
	require.NoError(t, err)
	require.Equal(t, body, manifest.Content)
	require.Equal(t, digest, manifest.Descriptor.Digest)
	require.Equal(t, digest, ocidigest.FromBytes(manifest.Content).String())
	require.Equal(t, int64(1), tokenRequests.Load())
	require.Positive(t, challengedRequests.Load())
	require.Positive(t, authorizedRequests.Load())
}

func TestClientFetchManifestRejectsDifferentPrivateBearerRealm(t *testing.T) {
	var targetRequests atomic.Int64
	targetAuthorization := make(chan string, 1)
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		recordAuthHeader(targetAuthorization, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		writeAuthTestBody(t, w, []byte(`{"token":"must-not-be-used"}`))
	}))
	defer targetServer.Close()
	privateRealm := privateCrossHostURL(t, targetServer.URL) + "/token"

	var challenges atomic.Int64
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		challenges.Add(1)
		w.Header().Set("WWW-Authenticate", bearerChallenge(privateRealm))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registryServer.Close()

	_, err := registrytool.NewClient().FetchManifest(t.Context(), registrytool.Reference{
		Registry: registryServer.URL, Repository: "library/node", Reference: "latest",
		Auth: registrytool.AuthConfig{Type: "basic", Username: "registry-user", Password: "registry-secret"},
	})
	require.Error(t, err)
	require.Positive(t, challenges.Load(), "registry must issue the challenged private realm")
	if receiveOptionalAuthHeader(targetAuthorization) != "" {
		t.Error("credential reached the rejected private token service")
	}
	require.Zero(t, targetRequests.Load(), "cross-host private token service must not be contacted")
}

func TestClientFetchManifestRejectsPrivateCrossHostTokenRedirectWithoutLeakingCredentials(t *testing.T) {
	var targetRequests atomic.Int64
	targetAuthorization := make(chan string, 1)
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		recordAuthHeader(targetAuthorization, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()
	redirectURL := privateCrossHostURL(t, targetServer.URL) + "/steal"
	var tokenRequests atomic.Int64
	tokenAuthorization := make(chan string, 1)
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenRequests.Add(1)
		recordAuthHeader(tokenAuthorization, r.Header.Get("Authorization"))
		http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	}))
	defer tokenServer.Close()

	registryAuthorization := make(chan string, 8)
	var challenges atomic.Int64
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registryAuthorization <- r.Header.Get("Authorization")
		challenges.Add(1)
		w.Header().Set("WWW-Authenticate", bearerChallenge(tokenServer.URL+"/token"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registryServer.Close()

	_, err := registrytool.NewClient().FetchManifest(t.Context(), registrytool.Reference{
		Registry: registryServer.URL, Repository: "library/node", Reference: "latest",
		Auth: registrytool.AuthConfig{Type: "basic", Username: "registry-user", Password: "registry-secret"},
	})
	require.Error(t, err)
	require.Empty(t, receiveAuthHeader(t, registryAuthorization))
	require.Positive(t, challenges.Load(), "registry must challenge before token redirect validation")
	require.Equal(t, int64(1), tokenRequests.Load(), "same-host token service must receive the request")
	require.Equal(t, "Basic cmVnaXN0cnktdXNlcjpyZWdpc3RyeS1zZWNyZXQ=", receiveAuthHeader(t, tokenAuthorization))
	if receiveOptionalAuthHeader(targetAuthorization) != "" {
		t.Error("credential reached the rejected redirect target")
	}
	require.Zero(t, targetRequests.Load(), "private cross-host redirect target must not be contacted")
}

func bearerChallenge(realm string) string {
	return fmt.Sprintf(`Bearer realm=%q,service="registry.test",scope="repository:library/node:pull"`, realm)
}

func privateCrossHostURL(t *testing.T, serverURL string) string {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	require.NoError(t, err)
	_, port, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)
	parsed.Host = net.JoinHostPort("localhost", port)
	return parsed.String()
}

func writeAuthTestManifest(t *testing.T, w http.ResponseWriter, r *http.Request, body []byte, digest string) {
	t.Helper()
	w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
	w.Header().Set(distribution.HeaderDockerContentDigest, digest)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodGet {
		writeAuthTestBody(t, w, body)
	}
}

func writeAuthTestBody(t *testing.T, w http.ResponseWriter, body []byte) {
	t.Helper()
	if _, err := w.Write(body); err != nil {
		t.Errorf("write authentication response: %v", err)
	}
}

func receiveAuthHeader(t *testing.T, headers <-chan string) string {
	t.Helper()
	select {
	case header := <-headers:
		return header
	default:
		t.Fatal("expected request did not reach its server")
		return ""
	}
}

func receiveOptionalAuthHeader(headers <-chan string) string {
	select {
	case header := <-headers:
		return header
	default:
		return ""
	}
}

func recordAuthHeader(headers chan<- string, header string) {
	select {
	case headers <- header:
	default:
	}
}

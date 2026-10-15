package upstream_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestConfiguredPrivateCredentialsRemainTerminalAfterBearerChallenge(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "user" || password != "pass" {
			t.Errorf("token basic auth = (%q, %q, %t), want configured credentials", username, password, ok)
		}
		writeString(t, w, `{"token":"private-token"}`)
	}))
	defer tokenServer.Close()

	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		if got := r.Header.Get(distribution.HeaderAuthorization); got == "Basic dXNlcjpwYXNz" {
			w.Header().Set(distribution.HeaderWWWAuthenticate, fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, tokenServer.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get(distribution.HeaderAuthorization); got != "Bearer private-token" {
			t.Errorf("registry authorization = %q, want configured bearer token", got)
		}
		w.WriteHeader(http.StatusForbidden)
		writeString(t, w, `{"errors":[{"code":"DENIED","message":"this image is not in the allowlist"}]}`)
	}))
	defer mirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(healthyManifestHandler(t, &primaryRequests))
	defer primary.Close()
	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry: primary.URL, Mirrors: []string{mirror.URL}, MirrorForbiddenPolicy: "public",
			Auth: upstream.AuthConfig{Type: "basic", Username: "user", Password: "pass"},
		},
	})

	_, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub", Repo: "library/nginx", Reference: "latest",
	})
	requireErrorStatus(t, err, http.StatusForbidden)
	requireEqual(t, mirrorRequests.Load(), int32(2), "mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(0), "primary requests")
}

func TestPublicMirrorAllowlistForbiddenAfterAnonymousBearerChallengeFailsOver(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeString(t, w, `{"token":"mirror-token"}`)
	}))
	defer tokenServer.Close()

	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		if r.Header.Get(distribution.HeaderAuthorization) == "" {
			w.Header().Set(distribution.HeaderWWWAuthenticate, fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, tokenServer.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get(distribution.HeaderAuthorization); got != "Bearer mirror-token" {
			t.Errorf("authorization = %q, want bearer token", got)
		}
		w.WriteHeader(http.StatusForbidden)
		writeString(t, w, `{"errors":[{"code":"DENIED","message":"this image is not in the allowlist"}]}`)
	}))
	defer mirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(healthyManifestHandler(t, &primaryRequests))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:              primary.URL,
			Mirrors:               []string{mirror.URL},
			MirrorForbiddenPolicy: "public",
		},
	})

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	requireEqual(t, mirrorRequests.Load(), int32(2), "mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(1), "primary requests")
}

func TestPublicMirrorAllowlistForbiddenAfterBearerChallengeHEADConfirmsWithGET(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeString(t, w, `{"token":"anonymous-token"}`)
	}))
	defer tokenServer.Close()

	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		if r.Header.Get(distribution.HeaderAuthorization) == "" {
			w.Header().Set(distribution.HeaderWWWAuthenticate, fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, tokenServer.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		if r.Method == http.MethodGet {
			writeString(t, w, `{"errors":[{"code":"DENIED","message":"this image is not in the allowlist"}]}`)
		}
	}))
	defer mirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(healthyManifestHandler(t, &primaryRequests))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {Registry: primary.URL, Mirrors: []string{mirror.URL}, MirrorForbiddenPolicy: "public"},
	})
	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub", Repo: "library/nginx", Reference: "latest", Method: http.MethodHead,
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	requireEqual(t, mirrorRequests.Load(), int32(3), "mirror requests (HEAD challenge, HEAD retry, GET confirmation)")
	requireEqual(t, primaryRequests.Load(), int32(1), "primary requests")
}

func TestPublicMirrorHEADEvidenceReadHasIndependentTimeout(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeString(t, w, `{"token":"anonymous-token"}`)
	}))
	defer tokenServer.Close()

	confirmationCanceled := make(chan struct{})
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusForbidden)
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Errorf("flush stalled confirmation response: %v", err)
				return
			}
			<-r.Context().Done()
			close(confirmationCanceled)
			return
		}
		if r.Header.Get(distribution.HeaderAuthorization) == "" {
			w.Header().Set(distribution.HeaderWWWAuthenticate, fmt.Sprintf(`Bearer realm="%s/token",service="registry.test"`, tokenServer.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer mirror.Close()

	primary := httptest.NewServer(healthyManifestHandler(t, new(atomic.Int32)))
	defer primary.Close()
	client := newTestClient(map[string]upstream.Config{
		"hub": {Registry: primary.URL, Mirrors: []string{mirror.URL}, MirrorForbiddenPolicy: "public"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	_, err := client.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: "hub", Repo: "library/nginx", Reference: "latest", Method: http.MethodHead,
	})
	elapsed := time.Since(started)
	requireErrorStatus(t, err, http.StatusForbidden)
	if elapsed >= 2*time.Second {
		t.Fatalf("HEAD allowlist evidence probe took %v; want an independent bounded timeout", elapsed)
	}
	select {
	case <-confirmationCanceled:
	case <-time.After(time.Second):
		t.Fatal("stalled confirmation GET was not canceled")
	}
}

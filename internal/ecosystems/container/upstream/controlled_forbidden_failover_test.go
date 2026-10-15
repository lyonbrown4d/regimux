package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestPublicMirrorForbiddenFailoverManifestMethods(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) { assertPublicMirrorManifestFallback(t, method) })
	}
}

func assertPublicMirrorManifestFallback(t *testing.T, method string) {
	t.Helper()
	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(publicAllowlistForbiddenHandler(t, &mirrorRequests))
	defer mirror.Close()
	var primaryRequests atomic.Int32
	primary := httptest.NewServer(publicManifestHandler(t, &primaryRequests))
	defer primary.Close()
	client := newTestClient(map[string]upstream.Config{
		"hub": {Registry: primary.URL, Mirrors: []string{mirror.URL}, MirrorForbiddenPolicy: "public", MirrorPolicy: "ordered"},
	})
	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub", Repo: "library/nginx", Reference: "latest", Method: method,
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	wantMirrorRequests := int32(1)
	if method == http.MethodHead {
		wantMirrorRequests++
	}
	requireEqual(t, mirrorRequests.Load(), wantMirrorRequests, "mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(1), "primary requests")
}

func publicAllowlistForbiddenHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get(distribution.HeaderAuthorization); got != "" {
			t.Errorf("mirror received authorization header %q", got)
		}
		w.WriteHeader(http.StatusForbidden)
		writeString(t, w, `{"errors":[{"code":"DENIED","message":"this image is not in the allowlist"}]}`)
	}
}

func publicManifestHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get(distribution.HeaderAuthorization); got != "" {
			t.Errorf("primary received leaked authorization header %q", got)
		}
		body := `{"schemaVersion":2}`
		w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:manifest")
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		writeString(t, w, body)
	}
}

func TestPublicMirrorForbiddenFailoverBlobMethods(t *testing.T) {
	const digest = "sha256:abcdef0123456789"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			var mirrorRequests atomic.Int32
			mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mirrorRequests.Add(1)
				w.WriteHeader(http.StatusForbidden)
				writeString(t, w, `{"errors":[{"code":"DENIED","message":"this image is not in the allowlist"}]}`)
			}))
			defer mirror.Close()

			var primaryRequests atomic.Int32
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				primaryRequests.Add(1)
				w.Header().Set(distribution.HeaderDockerContentDigest, digest)
				w.Header().Set(distribution.HeaderContentLength, "4")
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusOK)
					return
				}
				writeString(t, w, "blob")
			}))
			defer primary.Close()

			client := newTestClient(map[string]upstream.Config{
				"hub": {
					Registry:              primary.URL,
					Mirrors:               []string{mirror.URL},
					MirrorForbiddenPolicy: "public",
					MirrorPolicy:          "ordered",
				},
			})

			resp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
				UpstreamAlias: "hub",
				Repo:          "library/nginx",
				Digest:        digest,
				Method:        method,
			})
			requireNoError(t, err, "GetBlob")
			closeBody(t, resp.Body)
			wantMirrorRequests := int32(1)
			if method == http.MethodHead {
				wantMirrorRequests++
			}
			requireEqual(t, mirrorRequests.Load(), wantMirrorRequests, "mirror requests")
			requireEqual(t, primaryRequests.Load(), int32(1), "primary requests")
		})
	}
}

func TestForbiddenMirrorFailoverRemainsTerminalForAuthenticatedRequests(t *testing.T) {
	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorRequests.Add(1)
		if got := r.Header.Get(distribution.HeaderAuthorization); got == "" {
			t.Error("authenticated request did not include authorization header")
		}
		w.WriteHeader(http.StatusForbidden)
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
			Auth: upstream.AuthConfig{
				Type:     "basic",
				Username: "user",
				Password: "pass",
			},
		},
	})

	_, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireErrorStatus(t, err, http.StatusForbidden)
	requireEqual(t, mirrorRequests.Load(), int32(1), "mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(0), "primary requests")
}

func TestForbiddenMirrorFailoverIsTerminalByDefault(t *testing.T) {
	var mirrorRequests atomic.Int32
	mirror := httptest.NewServer(statusHandler(http.StatusForbidden, &mirrorRequests))
	defer mirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(healthyManifestHandler(t, &primaryRequests))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry: primary.URL,
			Mirrors:  []string{mirror.URL},
		},
	})

	_, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireErrorStatus(t, err, http.StatusForbidden)
	requireEqual(t, mirrorRequests.Load(), int32(1), "mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(0), "primary requests")
}

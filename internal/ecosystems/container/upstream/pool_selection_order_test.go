package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientManifestOrderedPolicyKeepsConfiguredOrderForHealthyEndpoints(t *testing.T) {
	t.Parallel()

	var slowMirrorPingRequests atomic.Int32
	var slowMirrorManifestRequests atomic.Int32
	slowMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			slowMirrorPingRequests.Add(1)
			time.Sleep(80 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		case "/v2/library/nginx/manifests/latest":
			slowMirrorManifestRequests.Add(1)
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
		default:
			t.Fatalf("unexpected path for slow mirror: %s", r.URL.Path)
		}
	}))
	defer slowMirror.Close()

	var primaryPingRequests atomic.Int32
	var primaryManifestRequests atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			primaryPingRequests.Add(1)
			w.WriteHeader(http.StatusOK)
		case "/v2/library/nginx/manifests/latest":
			primaryManifestRequests.Add(1)
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:def")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
		default:
			t.Fatalf("unexpected path for primary: %s", r.URL.Path)
		}
	}))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{slowMirror.URL},
			MirrorPolicy: "ordered",
			Probe: upstream.ProbeConfig{
				Enabled:  true,
				Timeout:  time.Second,
				Cooldown: time.Minute,
			},
		},
	})

	requireNoError(t, client.ProbeAlias(context.Background(), "hub"), "ProbeAlias")

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)

	requireEqual(t, slowMirrorPingRequests.Load(), int32(1), "slow mirror ping requests")
	requireEqual(t, slowMirrorManifestRequests.Load(), int32(1), "slow mirror manifest requests")
	requireEqual(t, primaryPingRequests.Load(), int32(1), "primary ping requests")
	requireEqual(t, primaryManifestRequests.Load(), int32(0), "primary manifest requests")
}

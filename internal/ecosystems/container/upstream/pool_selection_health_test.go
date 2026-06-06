package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientManifestOrderedPolicyPrefersHealthyLowestLatencyEndpoint(t *testing.T) {
	t.Parallel()

	var fastManifestRequests atomic.Int32
	var slowManifestRequests atomic.Int32
	fastMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/manifests/latest") {
			fastManifestRequests.Add(1)
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:fast")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
			return
		}
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected path for fast mirror: %s", r.URL.Path)
	}))
	defer fastMirror.Close()

	slowMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/manifests/latest") {
			slowManifestRequests.Add(1)
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:slow")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
			return
		}
		if r.URL.Path == "/v2/" {
			time.Sleep(80 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected path for slow mirror: %s", r.URL.Path)
	}))
	defer slowMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     slowMirror.URL,
			Mirrors:      []string{fastMirror.URL},
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

	requireEqual(t, fastManifestRequests.Load(), int32(1), "fast manifest requests")
	requireEqual(t, slowManifestRequests.Load(), int32(0), "slow manifest requests")
}

func TestClientManifestRoundRobinPolicyRotatesByHealthSortedCandidates(t *testing.T) {
	t.Parallel()

	var fastManifestRequests atomic.Int32
	var slowManifestRequests atomic.Int32
	fastMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/manifests/latest") {
			fastManifestRequests.Add(1)
		}
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2/library/nginx/manifests/latest":
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:fast")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
		default:
			t.Fatalf("unexpected path for fast mirror: %s", r.URL.Path)
		}
	}))
	defer fastMirror.Close()

	slowMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/manifests/latest") {
			slowManifestRequests.Add(1)
		}
		if r.URL.Path == "/v2/" {
			time.Sleep(80 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/v2/library/nginx/manifests/latest" {
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:slow")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
			return
		}
		t.Fatalf("unexpected path for slow mirror: %s", r.URL.Path)
	}))
	defer slowMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     slowMirror.URL,
			Mirrors:      []string{fastMirror.URL},
			MirrorPolicy: "round_robin",
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
	requireNoError(t, err, "first GetManifest")
	closeBody(t, resp.Body)

	resp, err = client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireNoError(t, err, "second GetManifest")
	closeBody(t, resp.Body)

	requireEqual(t, fastManifestRequests.Load(), int32(1), "fast manifest requests after second call")
	requireEqual(t, slowManifestRequests.Load(), int32(1), "slow manifest requests after second call")
}

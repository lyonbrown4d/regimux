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

	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientManifestOrderedPolicySkipsUnhealthyEndpointFromHealthCheck(t *testing.T) {
	t.Parallel()

	var coldMirrorRequests atomic.Int32
	coldMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coldMirrorRequests.Add(1)
		t.Helper()
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusBadGateway)
		case "/v2/library/nginx/manifests/latest":
			w.WriteHeader(http.StatusBadGateway)
		default:
			t.Fatalf("unexpected path for cold mirror: %s", r.URL.Path)
		}
	}))
	defer coldMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyMirrorRequests.Add(1)
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
			return
		case "/v2/library/nginx/manifests/latest":
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
		default:
			t.Fatalf("unexpected path for healthy mirror: %s", r.URL.Path)
		}
	}))
	defer healthyMirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests.Add(1)
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path for primary: %s", r.URL.Path)
		}
	}))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{coldMirror.URL, healthyMirror.URL},
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

	requireEqual(t, coldMirrorRequests.Load(), int32(1), "cold mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(2), "healthy mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(1), "primary requests")
}

func TestClientManifestOrderedPolicyFallsBackWhenAllEndpointsUnhealthy(t *testing.T) {
	t.Parallel()

	var firstRequests atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer first.Close()

	var secondRequests atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		secondRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer second.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{first.URL, second.URL},
			MirrorPolicy: "ordered",
		},
	})

	_, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireErrorStatus(t, err, http.StatusBadGateway)

	// Each endpoint receives one manifest request.
	requireEqual(t, firstRequests.Load(), int32(1), "first endpoint requests")
	requireEqual(t, secondRequests.Load(), int32(1), "second endpoint requests")
	requireEqual(t, primaryRequests.Load(), int32(1), "primary endpoint requests")
}

func TestClientManifestRoundRobinSkipsUnhealthyAndRotatesHealthyEndpoints(t *testing.T) {
	t.Parallel()

	var coldMirrorRequests atomic.Int32
	coldMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coldMirrorRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer coldMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyMirrorRequests.Add(1)
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/v2/library/nginx/manifests/latest" {
			body := `{"schemaVersion":2}`
			w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
			w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
			w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
			writeString(t, w, body)
			return
		}
		t.Fatalf("unexpected path for healthy mirror: %s", r.URL.Path)
	}))
	defer healthyMirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests.Add(1)
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path for primary: %s", r.URL.Path)
		}
	}))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{coldMirror.URL, healthyMirror.URL},
			MirrorPolicy: "round_robin",
			Probe: upstream.ProbeConfig{
				Enabled:  true,
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

	requireEqual(t, coldMirrorRequests.Load(), int32(1), "cold mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(2), "healthy mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(1), "primary requests")
}

func TestClientGetBlobSkipsUnhealthyBlobEndpointFromHealthCheck(t *testing.T) {
	t.Parallel()

	digest := "sha256:deadbeef"
	var coldMirrorRequests atomic.Int32
	coldMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coldMirrorRequests.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/v2/"):
			w.WriteHeader(http.StatusBadGateway)
		case strings.Contains(r.URL.Path, "/blobs/"+digest):
			w.WriteHeader(http.StatusBadGateway)
		default:
			t.Fatalf("unexpected path for cold mirror: %s", r.URL.Path)
		}
	}))
	defer coldMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthyMirrorRequests.Add(1)
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/v2/library/nginx/blobs/"+digest {
			w.Header().Set(distribution.HeaderDockerContentDigest, digest)
			w.Header().Set(distribution.HeaderContentLength, "4")
			writeString(t, w, "blob")
			return
		}
		t.Fatalf("unexpected path for healthy mirror: %s", r.URL.Path)
	}))
	defer healthyMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry: healthyMirror.URL,
			Mirrors:  []string{coldMirror.URL},
			Blob: upstream.BlobConfig{
				MaxConcurrentAttempts: 1,
			},
			Probe: upstream.ProbeConfig{
				Enabled:  true,
				Cooldown: time.Minute,
			},
		},
	})

	requireNoError(t, client.ProbeAlias(context.Background(), "hub"), "ProbeAlias")

	resp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "GetBlob")
	closeBody(t, resp.Body)

	requireEqual(t, coldMirrorRequests.Load(), int32(1), "cold mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(2), "healthy mirror requests")
}

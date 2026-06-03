package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestClientGetManifestFailsOverMirrors(t *testing.T) {
	t.Parallel()

	var failedMirrorRequests atomic.Int32
	failedMirror := httptest.NewServer(statusHandler(http.StatusBadGateway, &failedMirrorRequests))
	defer failedMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(healthyManifestHandler(t, &healthyMirrorRequests))
	defer healthyMirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(statusHandler(http.StatusInternalServerError, &primaryRequests))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{failedMirror.URL, healthyMirror.URL},
			MirrorPolicy: "ordered",
		},
	})

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	requireEqual(t, failedMirrorRequests.Load(), int32(1), "failed mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(1), "healthy mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(0), "primary requests")
}

func TestClientGetManifestDoesNotFailOverUnauthorizedMirror(t *testing.T) {
	t.Parallel()

	var unauthorizedMirrorRequests atomic.Int32
	unauthorizedMirror := httptest.NewServer(statusHandler(http.StatusUnauthorized, &unauthorizedMirrorRequests))
	defer unauthorizedMirror.Close()

	var secondMirrorRequests atomic.Int32
	secondMirror := httptest.NewServer(simpleManifestHandler(t, &secondMirrorRequests))
	defer secondMirror.Close()

	var primaryRequests atomic.Int32
	primary := httptest.NewServer(statusHandler(http.StatusOK, &primaryRequests))
	defer primary.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{unauthorizedMirror.URL, secondMirror.URL},
			MirrorPolicy: "ordered",
		},
	})

	_, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireErrorStatus(t, err, http.StatusUnauthorized)
	requireEqual(t, unauthorizedMirrorRequests.Load(), int32(1), "unauthorized mirror requests")
	requireEqual(t, secondMirrorRequests.Load(), int32(0), "second mirror requests")
	requireEqual(t, primaryRequests.Load(), int32(0), "primary requests")
}

func TestClientGetManifestFailsOverOnTransportError(t *testing.T) {
	t.Parallel()

	var timeoutMirrorRequests atomic.Int32
	timeoutMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeoutMirrorRequests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/library/nginx/manifests/latest", "timeout mirror manifest path")
		time.Sleep(20 * time.Millisecond)
	}))
	defer timeoutMirror.Close()

	var healthyRegistryRequests atomic.Int32
	healthyRegistry := httptest.NewServer(healthyManifestHandler(t, &healthyRegistryRequests))
	defer healthyRegistry.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry:     healthyRegistry.URL,
			Mirrors:      []string{timeoutMirror.URL},
			MirrorPolicy: "ordered",
			HTTP: upstream.HTTPConfig{
				Timeout: 10 * time.Millisecond,
			},
		},
	})

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	requireEqual(t, timeoutMirrorRequests.Load(), int32(1), "timeout mirror requests")
	requireEqual(t, healthyRegistryRequests.Load(), int32(1), "healthy registry requests")
}

func TestClientGetManifestRetriesTransientStatus(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/library/nginx/manifests/latest", "manifest path")
		if requests.Load() == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		body := `{"schemaVersion":2}`
		w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		writeString(t, w, body)
	}))
	defer registryServer.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry: registryServer.URL,
			HTTP: upstream.HTTPConfig{
				Retry: upstream.HTTPRetryConfig{
					Enabled:    true,
					MaxRetries: 1,
					WaitMin:    time.Millisecond,
					WaitMax:    time.Millisecond,
				},
			},
		},
	})

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	requireEqual(t, requests.Load(), int32(2), "registry requests")
}

func TestClientRoundRobinStartsOnNextMirror(t *testing.T) {
	t.Parallel()

	var firstRequests atomic.Int32
	first := httptest.NewServer(pingHandler(t, "first", &firstRequests))
	defer first.Close()

	var secondRequests atomic.Int32
	second := httptest.NewServer(pingHandler(t, "second", &secondRequests))
	defer second.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{first.URL, second.URL},
			MirrorPolicy: "round_robin",
		},
	})

	requireNoError(t, client.Ping(context.Background(), "hub"), "first Ping")
	requireNoError(t, client.Ping(context.Background(), "hub"), "second Ping")
	requireEqual(t, firstRequests.Load(), int32(1), "first requests")
	requireEqual(t, secondRequests.Load(), int32(1), "second requests")
}

func TestClientGetBlobStartsConcurrentBlobFailover(t *testing.T) {
	t.Parallel()

	const digest = "sha256:abcdef0123456789"
	var slowFailRequests atomic.Int32
	var fastSuccessRequests atomic.Int32

	slowFailStarted := make(chan struct{}, 1)
	slowFailRelease := make(chan struct{})
	blobBody := "live"

	slowFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slowFailRequests.Add(1)
		slowFailStarted <- struct{}{}
		<-slowFailRelease
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer slowFail.Close()

	fastSuccess := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fastSuccessRequests.Add(1)
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(blobBody)))
		writeString(t, w, blobBody)
	}))
	defer fastSuccess.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors: []string{slowFail.URL, fastSuccess.URL},
			Blob: upstream.BlobConfig{
				MaxConcurrentAttempts: 2,
			},
		},
	})

	type blobResult struct {
		resp *upstream.BlobResponse
		err  error
	}

	done := make(chan blobResult, 1)
	go func() {
		resp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
			UpstreamAlias: "hub",
			Repo:          "library/nginx",
			Digest:        digest,
		})
		done <- blobResult{resp: resp, err: err}
	}()

	select {
	case <-slowFailStarted:
	case <-time.After(time.Second):
		t.Fatal("slow endpoint did not receive request")
	}
	time.Sleep(20 * time.Millisecond)
	if got := fastSuccessRequests.Load(); got != 1 {
		t.Fatalf("expected fast endpoint to be attempted while slow endpoint is blocked, got %d", got)
	}
	close(slowFailRelease)

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("GetBlob: %v", result.err)
		}
		closeBody(t, result.resp.Body)
		requireEqual(t, fastSuccessRequests.Load(), int32(1), "fast endpoint requests")
		requireEqual(t, slowFailRequests.Load(), int32(1), "slow endpoint requests")
	case <-time.After(time.Second):
		t.Fatal("GetBlob did not return")
	}
}

func statusHandler(status int, requests *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(status)
	}
}

func healthyManifestHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/library/nginx/manifests/latest", "manifest path")
		body := `{"schemaVersion":2}`
		w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		writeString(t, w, body)
	}
}

func simpleManifestHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		t.Helper()
		requests.Add(1)
		writeString(t, w, `{"schemaVersion":2}`)
	}
}

func pingHandler(t *testing.T, label string, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/", label+" ping path")
		w.WriteHeader(http.StatusOK)
	}
}

func requireErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatal("error is nil")
	}
	list := distribution.FromError(err)
	if list == nil {
		t.Fatalf("error = %v, want distribution error", err)
		return
	}
	requireEqual(t, list.Status, status, "status")
}

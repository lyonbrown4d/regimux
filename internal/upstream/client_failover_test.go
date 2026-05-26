package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

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

	client := upstream.NewClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{failedMirror.URL, healthyMirror.URL},
			MirrorPolicy: "ordered",
		},
	}, nil)

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

	client := upstream.NewClient(map[string]upstream.Config{
		"hub": {
			Registry:     primary.URL,
			Mirrors:      []string{unauthorizedMirror.URL, secondMirror.URL},
			MirrorPolicy: "ordered",
		},
	}, nil)

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

func TestClientRoundRobinStartsOnNextMirror(t *testing.T) {
	t.Parallel()

	var firstRequests atomic.Int32
	first := httptest.NewServer(pingHandler(t, "first", &firstRequests))
	defer first.Close()

	var secondRequests atomic.Int32
	second := httptest.NewServer(pingHandler(t, "second", &secondRequests))
	defer second.Close()

	client := upstream.NewClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{first.URL, second.URL},
			MirrorPolicy: "round_robin",
		},
	}, nil)

	requireNoError(t, client.Ping(context.Background(), "hub"), "first Ping")
	requireNoError(t, client.Ping(context.Background(), "hub"), "second Ping")
	requireEqual(t, firstRequests.Load(), int32(1), "first requests")
	requireEqual(t, secondRequests.Load(), int32(1), "second requests")
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
		w.Header().Set("Docker-Content-Digest", "sha256:abc")
		w.Header().Set("Content-Type", distribution.MediaTypeDockerManifest)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
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
	}
	requireEqual(t, list.Status, status, "status")
}

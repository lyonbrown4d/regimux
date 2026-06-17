package upstream_test

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

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
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
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

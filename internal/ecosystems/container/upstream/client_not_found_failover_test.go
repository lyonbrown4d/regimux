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

func TestClientGetManifestFailsOverManifestNotFoundMirror(t *testing.T) {
	t.Parallel()

	var missingMirrorRequests atomic.Int32
	missingMirror := httptest.NewServer(statusHandler(http.StatusNotFound, &missingMirrorRequests))
	defer missingMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(headManifestHandler(t, &healthyMirrorRequests))
	defer healthyMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{missingMirror.URL, healthyMirror.URL},
			MirrorPolicy: "ordered",
		},
	})

	resp, err := client.GetManifest(context.Background(), upstream.GetManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Reference:     "latest",
		Method:        http.MethodHead,
	})
	requireNoError(t, err, "GetManifest")
	closeBody(t, resp.Body)
	requireEqual(t, missingMirrorRequests.Load(), int32(1), "missing mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(1), "healthy mirror requests")
}

func TestClientListTagsFailsOverNotFoundMirror(t *testing.T) {
	t.Parallel()

	var missingMirrorRequests atomic.Int32
	missingMirror := httptest.NewServer(statusHandler(http.StatusNotFound, &missingMirrorRequests))
	defer missingMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(tagsHandlerWithCounter(t, &healthyMirrorRequests))
	defer healthyMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{missingMirror.URL, healthyMirror.URL},
			MirrorPolicy: "ordered",
		},
	})

	resp, err := client.ListTags(context.Background(), upstream.ListTagsRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
	})
	requireNoError(t, err, "ListTags")
	closeBody(t, resp.Body)
	requireEqual(t, missingMirrorRequests.Load(), int32(1), "missing mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(1), "healthy mirror requests")
}

func TestClientGetReferrersFailsOverNotFoundMirror(t *testing.T) {
	t.Parallel()

	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var missingMirrorRequests atomic.Int32
	missingMirror := httptest.NewServer(statusHandler(http.StatusNotFound, &missingMirrorRequests))
	defer missingMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(referrersHandler(t, digest, &healthyMirrorRequests))
	defer healthyMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{missingMirror.URL, healthyMirror.URL},
			MirrorPolicy: "ordered",
		},
	})

	resp, err := client.GetReferrers(context.Background(), upstream.ReferrersRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "GetReferrers")
	closeBody(t, resp.Body)
	requireEqual(t, missingMirrorRequests.Load(), int32(1), "missing mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(1), "healthy mirror requests")
}

func TestClientGetManifestFailsOverUnsupportedMediaMirror(t *testing.T) {
	t.Parallel()

	var htmlMirrorRequests atomic.Int32
	htmlMirror := httptest.NewServer(htmlHandler(t, &htmlMirrorRequests))
	defer htmlMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(healthyManifestHandler(t, &healthyMirrorRequests))
	defer healthyMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{htmlMirror.URL, healthyMirror.URL},
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
	requireEqual(t, htmlMirrorRequests.Load(), int32(1), "html mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(1), "healthy mirror requests")
}

func TestClientGetReferrersFailsOverUnsupportedMediaMirror(t *testing.T) {
	t.Parallel()

	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var htmlMirrorRequests atomic.Int32
	htmlMirror := httptest.NewServer(htmlHandler(t, &htmlMirrorRequests))
	defer htmlMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(referrersHandler(t, digest, &healthyMirrorRequests))
	defer healthyMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{htmlMirror.URL, healthyMirror.URL},
			MirrorPolicy: "ordered",
		},
	})

	resp, err := client.GetReferrers(context.Background(), upstream.ReferrersRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "GetReferrers")
	closeBody(t, resp.Body)
	requireEqual(t, htmlMirrorRequests.Load(), int32(1), "html mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(1), "healthy mirror requests")
}

func headManifestHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.Method, http.MethodHead, "manifest method")
		requireEqual(t, r.URL.Path, "/v2/library/nginx/manifests/latest", "manifest path")
		w.Header().Set(distribution.HeaderDockerContentDigest, "sha256:abc")
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeDockerManifest)
		w.Header().Set(distribution.HeaderContentLength, "0")
	}
}

func htmlHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		t.Helper()
		requests.Add(1)
		body := `<html>not a registry response</html>`
		w.Header().Set(distribution.HeaderContentType, "text/html; charset=utf-8")
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		writeString(t, w, body)
	}
}

func tagsHandlerWithCounter(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	body := `{"name":"library/nginx","tags":["latest"]}`
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/library/nginx/tags/list", "tags path")
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeJSON)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		writeString(t, w, body)
	}
}

func referrersHandler(t *testing.T, digest string, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	body := `{"schemaVersion":2,"mediaType":"` + distribution.MediaTypeOCIIndex + `","manifests":[]}`
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		requireEqual(t, r.URL.Path, "/v2/library/nginx/referrers/"+digest, "referrers path")
		w.Header().Set(distribution.HeaderContentType, distribution.MediaTypeOCIIndex)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		writeString(t, w, body)
	}
}

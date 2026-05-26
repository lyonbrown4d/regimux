package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func TestClientGetBlobPreservesHeadRangeAndBearerToken(t *testing.T) {
	t.Parallel()

	const digest = "sha256:0123456789abcdef"
	registryServer := httptest.NewServer(blobHeadHandler(t, digest))
	defer registryServer.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Registry: registryServer.URL,
			Auth:     upstream.AuthConfig{Type: "bearer", Token: "static-token"},
		},
	})

	resp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
		Range:         &reference.HTTPRange{Start: 2, End: 5},
		Method:        http.MethodHead,
	})
	requireNoError(t, err, "GetBlob")
	closeBody(t, resp.Body)
	requireEqual(t, resp.StatusCode, http.StatusPartialContent, "blob status")
	requireEqual(t, resp.Digest, digest, "blob digest")
	requireEqual(t, resp.Size, int64(4), "blob size")
}

func TestClientListTagsBuildsRequestURL(t *testing.T) {
	t.Parallel()

	registryServer := httptest.NewServer(tagsHandler(t))
	defer registryServer.Close()

	client := newTestClient(map[string]upstream.Config{"hub": {Registry: registryServer.URL}})
	resp, err := client.ListTags(context.Background(), upstream.ListTagsRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		N:             "2",
		Last:          "old",
	})
	requireNoError(t, err, "ListTags")
	closeBody(t, resp.Body)
}

func blobHeadHandler(t *testing.T, digest string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requireEqual(t, r.Method, http.MethodHead, "method")
		requireEqual(t, r.URL.Path, "/v2/library/nginx/blobs/"+digest, "blob path")
		requireEqual(t, r.Header.Get("Range"), "bytes=2-5", "range")
		requireEqual(t, r.Header.Get("Authorization"), "Bearer static-token", "authorization")
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusPartialContent)
	}
}

func tagsHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requireEqual(t, r.URL.Path, "/v2/library/nginx/tags/list", "tags path")
		requireEqual(t, r.URL.Query().Get("n"), "2", "tags n")
		requireEqual(t, r.URL.Query().Get("last"), "old", "tags last")
		writeString(t, w, `{"name":"library/nginx","tags":["latest"]}`)
	}
}

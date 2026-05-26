package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

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

func TestClientGetBlobUsesBlobRoundRobinPolicy(t *testing.T) {
	t.Parallel()

	const digest = "sha256:abcdef0123456789"
	var firstRequests atomic.Int32
	first := httptest.NewServer(blobBodyHandler(t, digest, "first", 0, &firstRequests))
	defer first.Close()

	var secondRequests atomic.Int32
	second := httptest.NewServer(blobBodyHandler(t, digest, "second", 0, &secondRequests))
	defer second.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{first.URL, second.URL},
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "round_robin",
				TopN:         2,
			},
		},
	})

	firstResp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "first GetBlob")
	requireEqual(t, readAndClose(t, firstResp.Body), "first", "first blob body")

	secondResp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "second GetBlob")
	requireEqual(t, readAndClose(t, secondResp.Body), "second", "second blob body")
	requireEqual(t, firstRequests.Load(), int32(1), "first blob requests")
	requireEqual(t, secondRequests.Load(), int32(1), "second blob requests")
}

func TestClientGetBlobLatencyPolicyPrefersProbedEndpoint(t *testing.T) {
	t.Parallel()

	const digest = "sha256:0123456789abcdef"
	var slowRequests atomic.Int32
	slow := httptest.NewServer(blobBodyHandler(t, digest, "slow", 40*time.Millisecond, &slowRequests))
	defer slow.Close()

	var fastRequests atomic.Int32
	fast := httptest.NewServer(blobBodyHandler(t, digest, "fast", 0, &fastRequests))
	defer fast.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{slow.URL, fast.URL},
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "latency",
				TopN:         1,
			},
			Probe: upstream.ProbeConfig{
				Enabled:  true,
				Timeout:  time.Second,
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
	requireEqual(t, readAndClose(t, resp.Body), "fast", "blob body")
	requireEqual(t, slowRequests.Load(), int32(1), "slow probe requests")
	requireEqual(t, fastRequests.Load(), int32(2), "fast probe and blob requests")
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

func blobBodyHandler(t *testing.T, digest, body string, probeDelay time.Duration, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		requests.Add(1)
		if r.URL.Path == "/v2/" {
			if probeDelay > 0 {
				time.Sleep(probeDelay)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		requireEqual(t, r.URL.Path, "/v2/library/nginx/blobs/"+digest, "blob path")
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		writeString(t, w, body)
	}
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

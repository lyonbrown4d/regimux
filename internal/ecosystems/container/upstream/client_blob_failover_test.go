package upstream_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func TestClientGetBlobFailsOverBlobNotFoundMirror(t *testing.T) {
	t.Parallel()

	const digest = "sha256:abcdef0123456789"
	var missingMirrorRequests atomic.Int32
	missingMirror := httptest.NewServer(statusHandler(http.StatusNotFound, &missingMirrorRequests))
	defer missingMirror.Close()

	var healthyMirrorRequests atomic.Int32
	healthyMirror := httptest.NewServer(blobBodyHandler(t, digest, "live", 0, &healthyMirrorRequests))
	defer healthyMirror.Close()

	client := newTestClient(map[string]upstream.Config{
		"hub": {
			Mirrors:      []string{missingMirror.URL, healthyMirror.URL},
			MirrorPolicy: "ordered",
			Blob: upstream.BlobConfig{
				MirrorPolicy: "ordered",
			},
		},
	})

	resp, err := client.GetBlob(context.Background(), upstream.GetBlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/nginx",
		Digest:        digest,
	})
	requireNoError(t, err, "GetBlob")
	requireEqual(t, readAndClose(t, resp.Body), "live", "blob body")
	requireEqual(t, missingMirrorRequests.Load(), int32(1), "missing mirror requests")
	requireEqual(t, healthyMirrorRequests.Load(), int32(1), "healthy mirror requests")
}

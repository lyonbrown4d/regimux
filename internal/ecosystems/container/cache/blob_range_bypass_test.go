package cache_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestBlobTransferSelectsBypassRangeWhenOriginIgnoresIt(t *testing.T) {
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &rangeIgnoringBlobClient{fakeRegistryClient: &fakeRegistryClient{blobBody: body, blobDigest: digest}}
	proxy := newTestProxy(client, nil, nil, nil, config.DefaultConfig())
	response, err := proxy.Blobs().Get(t.Context(), cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/test", Digest: digest, Method: http.MethodGet,
		Range: &object.HTTPRange{Start: 2, End: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(readAndClose(t, response.Reader))
	if response.Status != http.StatusPartialContent || got != "2345" || response.Headers.Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("status=%d body=%q Content-Range=%q", response.Status, got, response.Headers.Get("Content-Range"))
	}
}

func TestBlobTransferRejectsMismatchedInitialPartialRange(t *testing.T) {
	digest := testDigestFor([]byte("0123456789"))
	client := &mismatchedRangeBlobClient{fakeRegistryClient: &fakeRegistryClient{blobDigest: digest}}
	proxy := newTestProxy(client, nil, nil, nil, config.DefaultConfig())
	_, err := proxy.Blobs().Get(t.Context(), cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/test", Digest: digest, Method: http.MethodGet,
		Range: &object.HTTPRange{Start: 2, End: 5},
	})
	if err == nil {
		t.Fatal("proxy accepted a partial response for a different range")
	}
}

type mismatchedRangeBlobClient struct{ *fakeRegistryClient }

func (c *mismatchedRangeBlobClient) GetBlob(context.Context, upstream.GetBlobRequest) (*upstream.BlobResponse, error) {
	return &upstream.BlobResponse{
		Body: io.NopCloser(strings.NewReader("0123")), Digest: c.blobDigest, Size: 4,
		StatusCode: http.StatusPartialContent,
		Headers: http.Header{
			"Content-Range":  {"bytes 0-3/10"},
			"Content-Length": {"4"},
		},
	}, nil
}

type rangeIgnoringBlobClient struct{ *fakeRegistryClient }

func (c *rangeIgnoringBlobClient) GetBlob(ctx context.Context, req upstream.GetBlobRequest) (*upstream.BlobResponse, error) {
	req.Range = nil
	return c.fakeRegistryClient.GetBlob(ctx, req)
}

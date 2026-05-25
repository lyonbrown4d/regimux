package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestBlobProxyCachesMissAndServesRangeHit(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := NewProxy(client, WithMetadata(metadata), WithObjects(objects))

	httpRange := &reference.HTTPRange{Start: 2, End: 5}
	first, err := proxy.Blobs().Get(ctx, BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Range:         httpRange,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	firstBody := readAndClose(t, first.Reader)
	if first.Cache != CacheMiss || first.Status != http.StatusPartialContent || string(firstBody) != "2345" {
		t.Fatalf("unexpected first result: cache=%s status=%d body=%q", first.Cache, first.Status, firstBody)
	}
	if got := first.Headers.Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("unexpected content range %q", got)
	}
	if got := first.Headers.Get("Content-Length"); got != "4" {
		t.Fatalf("unexpected content length %q", got)
	}

	second, err := proxy.Blobs().Get(ctx, BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	secondBody := readAndClose(t, second.Reader)
	if second.Cache != CacheHit || string(secondBody) != string(body) {
		t.Fatalf("unexpected second result: cache=%s body=%q", second.Cache, secondBody)
	}
	if client.blobGets != 1 {
		t.Fatalf("expected one upstream blob GET, got %d", client.blobGets)
	}

	head, err := proxy.Blobs().Get(ctx, BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodHead,
	})
	if err != nil {
		t.Fatalf("head blob get: %v", err)
	}
	headBody := readAndClose(t, head.Reader)
	if head.Cache != CacheHit || len(headBody) != 0 || head.Headers.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatalf("unexpected head result: cache=%s body=%q headers=%v", head.Cache, headBody, head.Headers)
	}
}

func TestTagProxyCachesAndRewritesLink(t *testing.T) {
	ctx := context.Background()
	client := &fakeRegistryClient{
		tagsBody: []byte(`{"name":"library/alpine","tags":["latest"]}`),
		tagsHeader: http.Header{
			"Link": {"<https://registry-1.docker.io/v2/library/alpine/tags/list?n=100&last=3.20>; rel=\"next\""},
		},
	}
	proxy := NewProxy(client, WithBackend(backend.NewMemory(backend.MemoryOptions{})), WithTagsTTL(time.Minute))

	first, err := proxy.Tags().List(ctx, TagRequest{UpstreamAlias: "hub", Repo: "library/alpine", N: "100"})
	if err != nil {
		t.Fatalf("first tags list: %v", err)
	}
	if first.Cache != CacheBypass {
		t.Fatalf("unexpected first cache status %s", first.Cache)
	}
	wantLink := "</v2/hub/library/alpine/tags/list?n=100&last=3.20>; rel=\"next\""
	if got := first.Headers.Get("Link"); got != wantLink {
		t.Fatalf("unexpected rewritten link %q", got)
	}

	second, err := proxy.Tags().List(ctx, TagRequest{UpstreamAlias: "hub", Repo: "library/alpine", N: "100"})
	if err != nil {
		t.Fatalf("second tags list: %v", err)
	}
	if second.Cache != CacheHit || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("unexpected second tags result: cache=%s body=%q", second.Cache, second.Body)
	}
	if client.tagsLists != 1 {
		t.Fatalf("expected one upstream tags request, got %d", client.tagsLists)
	}
}

func TestReferrersFallbackTagIsCached(t *testing.T) {
	ctx := context.Background()
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	client := &fakeRegistryClient{
		referrersErr:  distribution.ErrManifestUnknown.WithDetail("no referrers api"),
		manifestBody:  []byte(`{"schemaVersion":2,"manifests":[]}`),
		manifestMedia: distribution.MediaTypeOCIIndex,
	}
	proxy := NewProxy(
		client,
		WithBackend(backend.NewMemory(backend.MemoryOptions{})),
		WithReferrersTTL(time.Minute),
		WithReferrersFallbackTag(true),
	)

	first, err := proxy.Referrers().Get(ctx, ReferrerRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest})
	if err != nil {
		t.Fatalf("first referrers get: %v", err)
	}
	if first.Cache != CacheBypass || first.MediaType != distribution.MediaTypeOCIIndex {
		t.Fatalf("unexpected first referrers result: cache=%s media=%s", first.Cache, first.MediaType)
	}
	if client.manifestReference != "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected fallback reference %q", client.manifestReference)
	}

	second, err := proxy.Referrers().Get(ctx, ReferrerRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest})
	if err != nil {
		t.Fatalf("second referrers get: %v", err)
	}
	if second.Cache != CacheHit || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("unexpected second referrers result: cache=%s body=%q", second.Cache, second.Body)
	}
	if client.referrersGets != 1 || client.manifestGets != 1 {
		t.Fatalf("unexpected upstream calls: referrers=%d manifests=%d", client.referrersGets, client.manifestGets)
	}
}

func newTestStores(t *testing.T) (meta.Store, object.Store) {
	t.Helper()
	metadata, err := meta.OpenBbolt(t.TempDir()+"/regimux.db", nil)
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if err := metadata.Close(); err != nil {
			t.Fatalf("close metadata store: %v", err)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open object store: %v", err)
	}
	return metadata, objects
}

func readAndClose(t *testing.T, reader io.ReadCloser) []byte {
	t.Helper()
	body, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close reader: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

func testDigestFor(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type fakeRegistryClient struct {
	blobBody   []byte
	blobDigest string
	blobGets   int
	blobHeads  int

	tagsBody   []byte
	tagsHeader http.Header
	tagsLists  int

	referrersBody []byte
	referrersErr  error
	referrersGets int

	manifestBody      []byte
	manifestMedia     string
	manifestReference string
	manifestGets      int
}

func (c *fakeRegistryClient) Ping(context.Context, string) error {
	return nil
}

func (c *fakeRegistryClient) GetManifest(_ context.Context, req upstream.GetManifestRequest) (*upstream.ManifestResponse, error) {
	c.manifestGets++
	c.manifestReference = req.Reference
	return &upstream.ManifestResponse{
		Body:      io.NopCloser(bytes.NewReader(c.manifestBody)),
		Digest:    testDigestFor(c.manifestBody),
		MediaType: c.manifestMedia,
		Size:      int64(len(c.manifestBody)),
		Headers:   http.Header{"Content-Type": {c.manifestMedia}},
	}, nil
}

func (c *fakeRegistryClient) GetBlob(_ context.Context, req upstream.GetBlobRequest) (*upstream.BlobResponse, error) {
	body := c.blobBody
	switch req.Method {
	case http.MethodHead:
		c.blobHeads++
		body = nil
	default:
		c.blobGets++
		if req.Range != nil {
			return nil, errors.New("cache miss fetch should not forward client range")
		}
	}
	return &upstream.BlobResponse{
		Body:       io.NopCloser(bytes.NewReader(body)),
		Digest:     c.blobDigest,
		Size:       int64(len(c.blobBody)),
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Length": {strconv.Itoa(len(c.blobBody))},
			"Content-Type":   {"application/octet-stream"},
		},
	}, nil
}

func (c *fakeRegistryClient) ListTags(context.Context, upstream.ListTagsRequest) (*upstream.TagsResponse, error) {
	c.tagsLists++
	return &upstream.TagsResponse{
		Body:    io.NopCloser(bytes.NewReader(c.tagsBody)),
		Headers: c.tagsHeader.Clone(),
	}, nil
}

func (c *fakeRegistryClient) GetReferrers(context.Context, upstream.ReferrersRequest) (*upstream.ReferrersResponse, error) {
	c.referrersGets++
	if c.referrersErr != nil {
		return nil, c.referrersErr
	}
	return &upstream.ReferrersResponse{
		Body:      io.NopCloser(bytes.NewReader(c.referrersBody)),
		MediaType: distribution.MediaTypeOCIIndex,
		Headers:   http.Header{"Content-Type": {distribution.MediaTypeOCIIndex}},
	}, nil
}

var _ upstream.RegistryClient = (*fakeRegistryClient)(nil)

package cache_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func newTestProxy(client upstream.RegistryClient, metadata meta.Store, objects object.Store, cacheBackend backend.Backend, cfg config.Config) *cache.Proxy {
	return cache.NewProxy(cache.ProxyDependencies{
		Client:      client,
		Cache:       cacheBackend,
		Metadata:    metadata,
		Objects:     objects,
		CacheConfig: cfg.Cache,
	})
}

func TestBlobProxyCachesMissAndServesRangeHit(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, nil, config.Config{})

	httpRange := &reference.HTTPRange{Start: 2, End: 5}
	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Range:         httpRange,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	assertRangeBlobMiss(t, first)

	second, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertFullBlobHit(t, second, body)
	if client.blobGets != 1 {
		t.Fatalf("expected one upstream blob GET, got %d", client.blobGets)
	}

	head, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodHead,
	})
	if err != nil {
		t.Fatalf("head blob get: %v", err)
	}
	assertHeadBlobHit(t, head, len(body))
}

func TestBlobProxyTouchesBlobAccessOnLocalHit(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, nil, config.Config{})

	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	_ = readAndClose(t, first.Reader)

	old := time.Now().UTC().Add(-2 * time.Hour)
	_, err = metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       digest,
		Size:         int64(len(body)),
		MediaType:    "application/octet-stream",
		ObjectKey:    digest,
		LastAccessAt: old,
	})
	if err != nil {
		t.Fatalf("set old blob access: %v", err)
	}
	_, err = metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          "hub",
		Repository:     "library/alpine",
		Digest:         digest,
		LastAccessAt:   old,
		LastVerifiedAt: old,
	})
	if err != nil {
		t.Fatalf("set old repo blob access: %v", err)
	}

	hit, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertFullBlobHit(t, hit, body)

	assertBlobAccessTouched(ctx, t, metadata, digest, old)
}

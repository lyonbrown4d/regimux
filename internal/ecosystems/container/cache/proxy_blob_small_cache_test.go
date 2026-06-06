package cache_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestBlobProxyServesSmallBlobFromKVCache(t *testing.T) {
	ctx := context.Background()
	body := []byte("small-config")
	digest := testDigestFor(body)
	fixture := primeSmallBlobCache(t, body, 1024)

	second, err := fixture.proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertFullBlobHit(t, second, body)
	assertBlobRequestCounters(t, fixture.client, 1, 0)
}

func TestBlobProxyServesSmallBlobRangeFromKVCache(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	fixture := primeSmallBlobCache(t, body, 1024)

	ranged, err := fixture.proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Range:         &object.HTTPRange{Start: 2, End: 5},
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("range blob get: %v", err)
	}
	assertRangeBlobHit(t, ranged)
	assertBlobRequestCounters(t, fixture.client, 1, 0)
}

func TestBlobProxySkipsKVCacheForLargeBlob(t *testing.T) {
	ctx := context.Background()
	body := []byte("large-enough")
	digest := testDigestFor(body)
	fixture := primeSmallBlobCache(t, body, 4)

	second, err := fixture.proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertFullBlobMiss(t, second, body)
	assertBlobRequestCounters(t, fixture.client, 2, 0)
}

type smallBlobCacheFixture struct {
	proxy  *cache.Proxy
	client *fakeRegistryClient
}

func primeSmallBlobCache(t *testing.T, body []byte, maxSize int64) smallBlobCacheFixture {
	t.Helper()

	ctx := context.Background()
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	cacheBackend := backend.NewMemory(backend.MemoryOptions{})
	proxy := newTestProxy(client, metadata, objects, cacheBackend, smallBlobCacheConfig(maxSize))

	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	assertFullBlobMiss(t, first, body)
	if deleteErr := objects.Delete(ctx, digest); deleteErr != nil {
		t.Fatalf("delete object store copy: %v", deleteErr)
	}
	return smallBlobCacheFixture{proxy: proxy, client: client}
}

func smallBlobCacheConfig(maxSize int64) config.Config {
	return config.Config{
		Cache: config.CacheConfig{
			Blob: config.BlobCacheConfig{
				SmallCache: config.SmallBlobCacheConfig{
					Enabled:      true,
					MaxSizeBytes: maxSize,
					TTL:          time.Hour,
				},
			},
		},
	}
}

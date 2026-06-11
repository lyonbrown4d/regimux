package cache_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestBlobProxyFillsRangeMissWhenStreamAndCacheEnabled(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		nil,
		config.Config{
			Cache: config.CacheConfig{
				Blob: config.BlobCacheConfig{
					StreamAndCache: true,
				},
			},
		},
	)

	httpRange := &object.HTTPRange{Start: 2, End: 5}
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
	assertBlobRequestCounters(t, client, 1, 0)
	assertObjectPresence(ctx, t, objects, digest, true)
	blob := requireBlobRecord(ctx, t, metadata, digest)
	if blob.Size != int64(len(body)) {
		t.Fatalf("blob metadata size = %d, want %d", blob.Size, len(body))
	}
	requireRepoBlobRecord(ctx, t, metadata, digest)

	second, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Range:         httpRange,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertRangeBlobHit(t, second)
	assertBlobRequestCounters(t, client, 1, 0)
}

func TestBlobProxyHeadMissBypassesStoreWhenStreamAndCacheEnabled(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		nil,
		config.Config{
			Cache: config.CacheConfig{
				Blob: config.BlobCacheConfig{
					StreamAndCache: true,
				},
			},
		},
	)

	head, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodHead,
	})
	if err != nil {
		t.Fatalf("head blob get: %v", err)
	}
	bodyBuf := readAndClose(t, head.Reader)
	if head.Cache != cache.CacheBypass || head.Status != http.StatusOK || len(bodyBuf) != 0 {
		t.Fatalf("unexpected head result: cache=%s status=%d body=%q", head.Cache, head.Status, bodyBuf)
	}
	assertBlobRequestCounters(t, client, 0, 1)
	assertObjectPresence(ctx, t, objects, digest, false)
}

func TestBlobProxySkipsVerifyForRecentSharedBlobWithinTTL(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	verifyTTL := 5 * time.Minute
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		nil,
		config.Config{
			Cache: config.CacheConfig{
				Blob: config.BlobCacheConfig{
					VerifyTTL: verifyTTL,
				},
			},
		},
	)

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
	waitObjectStored(ctx, t, objects, digest)
	if first.Cache != cache.CacheMiss {
		t.Fatalf("first cache status = %s, want miss", first.Cache)
	}
	assertBlobRequestCounters(t, client, 1, 0)
	setRepoBlobVerifiedAt(ctx, t, metadata, digest, time.Now().UTC().Add(-verifyTTL/2))

	second, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	_ = readAndClose(t, second.Reader)
	if second.Cache != cache.CacheHit {
		t.Fatalf("second cache status = %s, want hit", second.Cache)
	}
	assertBlobRequestCounters(t, client, 1, 0)
	setRepoBlobVerifiedAt(ctx, t, metadata, digest, time.Now().UTC().Add(-(verifyTTL + time.Second)))

	third, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("third blob get: %v", err)
	}
	_ = readAndClose(t, third.Reader)
	if third.Cache != cache.CacheHit {
		t.Fatalf("third cache status = %s, want hit", third.Cache)
	}
	assertBlobRequestCounters(t, client, 1, 1)
}

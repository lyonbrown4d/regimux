package cache_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
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

func TestBlobProxyConcurrentFullMissWaitsForStreamedFill(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	reader := newBlockingBlobReader(body)
	client := &fakeRegistryClient{blobBody: body, blobReader: reader, blobDigest: digest}
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

	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	if first.Cache != cache.CacheMiss {
		t.Fatalf("first cache status = %s, want miss", first.Cache)
	}

	secondCh := make(chan blobGetResult, 1)
	go func() {
		result, getErr := proxy.Blobs().Get(ctx, cache.BlobRequest{
			UpstreamAlias: "hub",
			Repo:          "library/alpine",
			Digest:        digest,
			Method:        http.MethodGet,
		})
		secondCh <- blobGetResult{result: result, err: getErr}
	}()

	select {
	case second := <-secondCh:
		t.Fatalf("second blob get returned before streamed fill completed: result=%#v err=%v", second.result, second.err)
	case <-time.After(100 * time.Millisecond):
	}

	reader.Release()
	assertFullBlobMiss(t, first, body)
	waitObjectStored(ctx, t, objects, digest)

	var second blobGetResult
	select {
	case second = <-secondCh:
	case <-time.After(time.Second):
		t.Fatal("second blob get did not resume after streamed fill completed")
	}
	if second.err != nil {
		t.Fatalf("second blob get: %v", second.err)
	}
	assertFullBlobHit(t, second.result, body)
	assertBlobRequestCounters(t, client, 1, 0)
}

func TestBlobProxyDistributedFullMissWaitsForStreamedFill(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	reader := newBlockingBlobReader(body)
	client := &fakeRegistryClient{blobBody: body, blobReader: reader, blobDigest: digest}
	metadata, objects := newTestStores(t)
	leases := newSharedLeaseBackend()
	cfg := config.Config{
		Cache: config.CacheConfig{
			Blob: config.BlobCacheConfig{
				StreamAndCache: true,
			},
		},
	}
	left := newTestProxy(client, metadata, objects, leases, cfg)
	right := newTestProxy(client, metadata, objects, leases, cfg)

	first, err := left.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	if first.Cache != cache.CacheMiss {
		t.Fatalf("first cache status = %s, want miss", first.Cache)
	}

	secondCh := make(chan blobGetResult, 1)
	go func() {
		result, getErr := right.Blobs().Get(ctx, cache.BlobRequest{
			UpstreamAlias: "hub",
			Repo:          "library/alpine",
			Digest:        digest,
			Method:        http.MethodGet,
		})
		secondCh <- blobGetResult{result: result, err: getErr}
	}()

	select {
	case second := <-secondCh:
		t.Fatalf("second blob get returned before distributed streamed fill completed: result=%#v err=%v", second.result, second.err)
	case <-time.After(100 * time.Millisecond):
	}

	reader.Release()
	assertFullBlobMiss(t, first, body)

	var second blobGetResult
	select {
	case second = <-secondCh:
	case <-time.After(time.Second):
		t.Fatal("second blob get did not resume after distributed streamed fill completed")
	}
	if second.err != nil {
		t.Fatalf("second blob get: %v", second.err)
	}
	assertFullBlobHit(t, second.result, body)
	assertBlobRequestCounters(t, client, 1, 0)
}

func TestBlobProxyDistributedRangeMissWaitsForStreamedFill(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	reader := newBlockingBlobReader(body)
	client := &fakeRegistryClient{blobBody: body, blobReader: reader, blobDigest: digest}
	metadata, objects := newTestStores(t)
	leases := newSharedLeaseBackend()
	cfg := config.Config{
		Cache: config.CacheConfig{
			Blob: config.BlobCacheConfig{
				StreamAndCache: true,
			},
		},
	}
	left := newTestProxy(client, metadata, objects, leases, cfg)
	right := newTestProxy(client, metadata, objects, leases, cfg)

	first, err := left.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}

	rangeCh := make(chan blobGetResult, 1)
	go func() {
		result, getErr := right.Blobs().Get(ctx, cache.BlobRequest{
			UpstreamAlias: "hub",
			Repo:          "library/alpine",
			Digest:        digest,
			Range:         &object.HTTPRange{Start: 2, End: 5},
			Method:        http.MethodGet,
		})
		rangeCh <- blobGetResult{result: result, err: getErr}
	}()

	select {
	case ranged := <-rangeCh:
		t.Fatalf("range blob get returned before distributed streamed fill completed: result=%#v err=%v", ranged.result, ranged.err)
	case <-time.After(100 * time.Millisecond):
	}

	reader.Release()
	assertFullBlobMiss(t, first, body)

	var ranged blobGetResult
	select {
	case ranged = <-rangeCh:
	case <-time.After(time.Second):
		t.Fatal("range blob get did not resume after distributed streamed fill completed")
	}
	if ranged.err != nil {
		t.Fatalf("range blob get: %v", ranged.err)
	}
	assertRangeBlobHit(t, ranged.result)
	assertBlobRequestCounters(t, client, 1, 0)
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

type sharedLeaseBackend struct {
	backend.Noop
	mu    sync.Mutex
	locks map[string]string
	next  int
}

func newSharedLeaseBackend() *sharedLeaseBackend {
	return &sharedLeaseBackend{locks: map[string]string{}}
}

func (b *sharedLeaseBackend) AcquireLease(_ context.Context, key string, _ time.Duration) (backend.Lease, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.locks[key]; ok {
		return nil, false, nil
	}
	b.next++
	token := key + "#" + string(rune('a'+b.next))
	b.locks[key] = token
	return &sharedLease{backend: b, key: key, token: token}, true, nil
}

type sharedLease struct {
	backend *sharedLeaseBackend
	key     string
	token   string
}

func (l *sharedLease) Release(context.Context) error {
	l.backend.mu.Lock()
	defer l.backend.mu.Unlock()
	if l.backend.locks[l.key] == l.token {
		delete(l.backend.locks, l.key)
	}
	return nil
}

func (l *sharedLease) Extend(context.Context, time.Duration) error {
	return nil
}

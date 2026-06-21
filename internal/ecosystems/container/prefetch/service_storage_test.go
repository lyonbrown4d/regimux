package prefetch_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServiceRunSkipsRecentlySuccessfulPrefetch(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)

	manifestDigest := testDigest("a")
	configDigest := testDigest("b")
	layerDigest := testDigest("c")
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(manifestDigest, distribution.MediaTypeOCIManifest, imageManifestBody(t, configDigest, layerDigest)),
	})
	opts := defaultRunOptions()
	opts.Now = time.Now().UTC().Truncate(time.Second)
	opts.RetryWindow = time.Hour

	firstBlobs := &fakeBlobService{}
	first, err := runPrefetchWithOptions(ctx, t, store, manifests, firstBlobs, opts)
	if err != nil {
		t.Fatalf("first prefetch: %v", err)
	}
	assertReport(t, first, 1, 0)
	assertBlobRequests(t, firstBlobs.requestSnapshot(), []string{configDigest, layerDigest})

	opts.Now = opts.Now.Add(30 * time.Minute)
	secondBlobs := &fakeBlobService{}
	second, err := runPrefetchWithOptions(ctx, t, store, manifests, secondBlobs, opts)
	if err != nil {
		t.Fatalf("second prefetch: %v", err)
	}
	if second.Prefetched != 0 || second.SkippedCandidates != 1 || second.Failed != 0 {
		t.Fatalf("expected second run to skip recent success: %#v", second)
	}
	assertBlobRequests(t, secondBlobs.requestSnapshot(), nil)
}

func TestServiceRunCountsBytesWarmedOnlyForCacheMiss(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)

	configDigest := testDigest("b")
	layerDigest := testDigest("c")
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(testDigest("a"), distribution.MediaTypeOCIManifest, imageManifestBody(t, configDigest, layerDigest)),
	})
	blobs := &fakeBlobService{status: cache.CacheHit}

	report, err := runPrefetch(ctx, t, store, manifests, blobs)
	if err != nil {
		t.Fatalf("run prefetch: %v", err)
	}
	assertReport(t, report, 1, 0)
	if report.BytesWarmed != 0 {
		t.Fatalf("bytes warmed = %d, want 0 for cache hits", report.BytesWarmed)
	}
	assertBlobRequests(t, blobs.requestSnapshot(), []string{configDigest, layerDigest})
	assertClosedBlobReaders(t, blobs.closedSnapshot(), []string{configDigest, layerDigest})
}

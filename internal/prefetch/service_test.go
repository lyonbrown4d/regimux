package prefetch_test

import (
	"context"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

const (
	testAlias = "hub"
	testRepo  = "library/node"
	sourceTag = "20"
	targetTag = "25"
)

func TestServiceRunPrefetchesImageManifestBlobs(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)

	manifestDigest := testDigest("a")
	configDigest := testDigest("b")
	layerDigest := testDigest("c")
	secondLayerDigest := testDigest("d")
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(manifestDigest, distribution.MediaTypeOCIManifest, imageManifestBody(t, configDigest, layerDigest, secondLayerDigest)),
	})
	blobs := &fakeBlobService{}

	report, err := runPrefetch(ctx, t, store, manifests, blobs)
	if err != nil {
		t.Fatalf("run prefetch: %v", err)
	}
	assertReport(t, report, 1, 0)
	assertManifestReferences(t, manifests.requestSnapshot(), []string{targetTag})
	assertBlobRequests(t, blobs.requestSnapshot(), []string{configDigest, layerDigest, secondLayerDigest})
	assertClosedBlobReaders(t, blobs.closedSnapshot(), []string{configDigest, layerDigest, secondLayerDigest})
}

func TestServiceRunPrefetchesIndexChildManifestBlobs(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)

	indexDigest := testDigest("e")
	childDigest := testDigest("f")
	configDigest := testDigest("1")
	layerDigest := testDigest("2")
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag:   cachedManifest(indexDigest, distribution.MediaTypeOCIIndex, indexManifestBody(t, childDigest)),
		childDigest: cachedManifest(childDigest, distribution.MediaTypeDockerManifest, imageManifestBody(t, configDigest, layerDigest)),
	})
	blobs := &fakeBlobService{}

	report, err := runPrefetch(ctx, t, store, manifests, blobs)
	if err != nil {
		t.Fatalf("run prefetch: %v", err)
	}
	assertReport(t, report, 1, 0)
	assertManifestReferences(t, manifests.requestSnapshot(), []string{targetTag, childDigest})
	assertBlobRequests(t, blobs.requestSnapshot(), []string{configDigest, layerDigest})
	assertClosedBlobReaders(t, blobs.closedSnapshot(), []string{configDigest, layerDigest})
}

func TestServiceRunCountsManifestParseFailure(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(testDigest("3"), distribution.MediaTypeOCIManifest, []byte("{")),
	})

	report, err := runPrefetch(ctx, t, store, manifests, &fakeBlobService{})
	if err != nil {
		t.Fatalf("run prefetch: %v", err)
	}
	assertReport(t, report, 0, 1)
}

func TestServiceRunCountsMissingBlobService(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(testDigest("4"), distribution.MediaTypeOCIManifest, imageManifestBody(t, testDigest("5"), testDigest("6"))),
	})

	report, err := runPrefetch(ctx, t, store, manifests, nil)
	if err != nil {
		t.Fatalf("run prefetch: %v", err)
	}
	assertReport(t, report, 0, 1)
}

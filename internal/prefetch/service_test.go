package prefetch_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
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

func TestServiceRunManifestOnlySkipsBlobRequests(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)

	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(testDigest("x"), distribution.MediaTypeOCIManifest, imageManifestBody(t, testDigest("y"), testDigest("z"))),
	})
	opts := defaultRunOptions()
	opts.ManifestOnly = true

	report, err := runPrefetchWithOptions(ctx, t, store, manifests, nil, opts)
	if err != nil {
		t.Fatalf("run manifest-only prefetch: %v", err)
	}
	assertReport(t, report, 1, 0)
	assertManifestReferences(t, manifests.requestSnapshot(), []string{targetTag})
}

func TestServiceRunRespectsByteBudgetAndRecordsHistory(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(testDigest("a"), distribution.MediaTypeOCIManifest, imageManifestBody(t, testDigest("b"), testDigest("c"))),
	})
	blobs := &fakeBlobService{}
	opts := defaultRunOptions()
	opts.MaxBytes = 100

	report, err := runPrefetchWithOptions(ctx, t, store, manifests, blobs, opts)
	if err != nil {
		t.Fatalf("run prefetch: %v", err)
	}
	if report.Prefetched != 0 || report.SkippedCandidates != 1 || report.Failed != 0 {
		t.Fatalf("unexpected byte budget report: %#v", report)
	}
	assertBlobRequests(t, blobs.requestSnapshot(), nil)
	assertPrefetchHistory(t, store, "completed", "skipped")
}

func TestServiceRunAppliesFailureBackoffAndRetryControl(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(testDigest("d"), distribution.MediaTypeOCIManifest, []byte("{")),
	})
	opts := defaultRunOptions()
	opts.FailureBackoff = time.Hour
	opts.RetryWindow = 24 * time.Hour

	first, err := runPrefetchWithOptions(ctx, t, store, manifests, &fakeBlobService{}, opts)
	if err != nil {
		t.Fatalf("first prefetch: %v", err)
	}
	if first.Failed != 1 {
		t.Fatalf("expected first run to fail: %#v", first)
	}

	second, err := runPrefetchWithOptions(ctx, t, store, manifests, &fakeBlobService{}, opts)
	if err != nil {
		t.Fatalf("second prefetch: %v", err)
	}
	if second.SkippedCandidates != 1 || second.Failed != 0 {
		t.Fatalf("expected second run to back off: %#v", second)
	}

	service := prefetch.NewService(prefetch.ServiceDependencies{Metadata: store})
	if _, retryErr := service.RetryPrefetch(ctx); retryErr != nil {
		t.Fatalf("request retry: %v", retryErr)
	}
	third, err := runPrefetchWithOptions(ctx, t, store, manifests, &fakeBlobService{}, opts)
	if err != nil {
		t.Fatalf("third prefetch: %v", err)
	}
	if !third.RetryRequested || third.Failed != 1 {
		t.Fatalf("expected retry to bypass backoff: %#v", third)
	}
}

func TestServiceRunConsumesCancelControl(t *testing.T) {
	ctx := context.Background()
	store := newPrefetchMetaStore(ctx, t)
	recordObservedPull(ctx, t, store)
	service := prefetch.NewService(prefetch.ServiceDependencies{Metadata: store})
	if _, err := service.CancelPrefetch(ctx); err != nil {
		t.Fatalf("request cancel: %v", err)
	}

	report, err := runPrefetchWithOptions(ctx, t, store, newFakeManifestService(nil), &fakeBlobService{}, defaultRunOptions())
	if err != nil {
		t.Fatalf("run prefetch: %v", err)
	}
	if !report.Canceled {
		t.Fatalf("expected canceled report: %#v", report)
	}
	assertPrefetchRunStatus(t, store, "canceled")
}

func TestServiceSyncPrefetchesImageManifestBlobs(t *testing.T) {
	ctx := context.Background()
	manifestDigest := testDigest("7")
	configDigest := testDigest("8")
	layerDigest := testDigest("9")
	manifests := newFakeManifestService(map[string]*cache.CachedManifest{
		targetTag: cachedManifest(manifestDigest, distribution.MediaTypeOCIManifest, imageManifestBody(t, configDigest, layerDigest)),
	})
	blobs := &fakeBlobService{}
	service := prefetch.NewService(prefetch.ServiceDependencies{
		Manifests: manifests,
		Blobs:     blobs,
	})

	report, err := service.Sync(ctx, prefetch.SyncOptions{
		Alias:     testAlias,
		Repo:      testRepo,
		Reference: targetTag,
	})
	if err != nil {
		t.Fatalf("sync prefetch: %v", err)
	}
	if report.ManifestDigest != manifestDigest || report.BlobCount != 2 || report.LayerCount != 1 {
		t.Fatalf("unexpected sync report: %#v", report)
	}
	assertManifestReferences(t, manifests.requestSnapshot(), []string{targetTag})
	assertBlobRequests(t, blobs.requestSnapshot(), []string{configDigest, layerDigest})
	assertClosedBlobReaders(t, blobs.closedSnapshot(), []string{configDigest, layerDigest})
}

func assertPrefetchHistory(t *testing.T, store meta.Store, runStatus, outcomeStatus string) {
	t.Helper()
	assertPrefetchRunStatus(t, store, runStatus)
	outcomes, err := store.ListPrefetchOutcomes(context.Background(), meta.PrefetchOutcomeListRecentFirst(), meta.PrefetchOutcomeListLimit(1))
	if err != nil {
		t.Fatalf("list prefetch outcomes: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != outcomeStatus {
		t.Fatalf("unexpected prefetch outcomes: %#v", outcomes)
	}
}

func assertPrefetchRunStatus(t *testing.T, store meta.Store, status string) {
	t.Helper()
	runs, err := store.ListPrefetchRuns(context.Background(), meta.PrefetchRunListRecentFirst(), meta.PrefetchRunListLimit(1))
	if err != nil {
		t.Fatalf("list prefetch runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != status {
		t.Fatalf("unexpected prefetch runs: %#v", runs)
	}
}

package cache_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestCleanupServiceDeletesStaleOrphanBlob(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)
	body := []byte("stale orphan blob")
	digest := putCleanupBlob(ctx, t, metadata, objects, body, now.Add(-48*time.Hour))

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor: 24 * time.Hour,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if report.DeletedBlobs != 1 || report.BytesDeleted != int64(len(body)) || report.DeletedDigests[0] != digest {
		t.Fatalf("unexpected cleanup report: %#v", report)
	}
	ok, err := objects.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("object exists: %v", err)
	}
	if ok {
		t.Fatal("expected stale orphan object to be deleted")
	}
	_, ok, err = metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil {
		t.Fatalf("blob metadata lookup: %v", err)
	}
	if ok {
		t.Fatal("expected stale orphan blob metadata to be deleted")
	}
}

func TestCleanupServiceKeepsRecentAndManifestProtectedBlobs(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)
	recentDigest := putCleanupBlob(ctx, t, metadata, objects, []byte("recent"), now.Add(-time.Hour))
	repoOnlyDigest := putCleanupBlob(ctx, t, metadata, objects, []byte("repo only"), now.Add(-48*time.Hour))
	manifestProtectedDigest := putCleanupBlob(ctx, t, metadata, objects, []byte("manifest protected"), now.Add(-48*time.Hour))

	_, err := metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:      "hub",
		Repository: "library/alpine",
		Digest:     repoOnlyDigest,
	})
	if err != nil {
		t.Fatalf("upsert repo blob: %v", err)
	}
	_, err = metadata.UpsertManifest(ctx, meta.ManifestRecord{
		Alias:      "hub",
		Repository: "library/alpine",
		Digest:     manifestProtectedDigest,
		MediaType:  distribution.MediaTypeOCIManifest,
		Size:       int64(len("manifest protected")),
		ObjectKey:  manifestProtectedDigest,
	})
	if err != nil {
		t.Fatalf("upsert manifest: %v", err)
	}

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor: 24 * time.Hour,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	assertCleanupReportCounts(t, report, 1, 1, 1)
	assertObjectsExist(ctx, t, objects, recentDigest, manifestProtectedDigest)
	assertObjectMissing(ctx, t, objects, repoOnlyDigest)
}

func TestCleanupServiceRespectsScanLimit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)

	putCleanupBlob(ctx, t, metadata, objects, []byte("first"), now.Add(-48*time.Hour))
	putCleanupBlob(ctx, t, metadata, objects, []byte("second"), now.Add(-48*time.Hour))
	putCleanupBlob(ctx, t, metadata, objects, []byte("third"), now.Add(-48*time.Hour))

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor: 24 * time.Hour,
		MaxScan:   2,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if report.ScannedBlobs != 2 || report.DeletedBlobs != 2 {
		t.Fatalf("unexpected cleanup report: %#v", report)
	}
	if !report.LimitReached {
		t.Fatalf("expected scan limit reached")
	}
	remaining, err := metadata.ListBlobs(ctx)
	if err != nil {
		t.Fatalf("list blobs: %v", err)
	}
	if got := remaining.Len(); got != 1 {
		t.Fatalf("remaining blobs = %d, want 1", got)
	}
}

func TestCleanupServiceReclaimsOldestBlobsAboveCapacity(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)

	oldest := putCleanupBlob(ctx, t, metadata, objects, []byte("1111"), now.Add(-3*time.Hour))
	middle := putCleanupBlob(ctx, t, metadata, objects, []byte("2222"), now.Add(-2*time.Hour))
	newest := putCleanupBlob(ctx, t, metadata, objects, []byte("3333"), now.Add(-time.Hour))

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:   168 * time.Hour,
		MaxBytes:    10,
		TargetBytes: 5,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if !report.CapacityExceeded || report.BytesBefore != 12 || report.BytesAfter != 4 || report.BytesTarget != 5 {
		t.Fatalf("unexpected capacity report: %#v", report)
	}
	if report.DeletedBlobs != 2 || report.DeletedDigests[0] != oldest || report.DeletedDigests[1] != middle {
		t.Fatalf("unexpected deleted blobs: %#v", report)
	}
	assertObjectsExist(ctx, t, objects, newest)
	assertObjectMissing(ctx, t, objects, oldest)
	assertObjectMissing(ctx, t, objects, middle)
}

func TestCleanupServiceReclaimsMissingAccessTimeLargeBlobsFirstAboveCapacity(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)

	recent := putCleanupBlob(ctx, t, metadata, objects, []byte("22"), now.Add(-time.Hour))
	old := putCleanupBlob(ctx, t, metadata, objects, []byte("3333"), now.Add(-48*time.Hour))
	neverAccessed := putCleanupBlob(ctx, t, metadata, objects, []byte("11111111"), time.Time{})

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:   168 * time.Hour,
		MaxBytes:    10,
		TargetBytes: 5,
		MaxDeletes:  1,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if report.DeletedBlobs != 1 || report.DeletedDigests[0] != neverAccessed {
		t.Fatalf("unexpected deleted blobs: %#v", report)
	}
	assertObjectMissing(ctx, t, objects, neverAccessed)
	assertObjectsExist(ctx, t, objects, recent, old)
}

func TestCleanupServiceSkipsCapacityWhenBelowMaxBytes(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)

	digest := putCleanupBlob(ctx, t, metadata, objects, []byte("small"), now.Add(-time.Hour))
	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:   168 * time.Hour,
		MaxBytes:    10,
		TargetBytes: 5,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if report.CapacityExceeded || report.DeletedBlobs != 0 || report.BytesAfter != 5 {
		t.Fatalf("unexpected cleanup report: %#v", report)
	}
	assertObjectsExist(ctx, t, objects, digest)
}

func TestCleanupServiceDryRunPlansCapacityDeletesWithoutDeleting(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)

	oldest := putCleanupBlob(ctx, t, metadata, objects, []byte("1111"), now.Add(-3*time.Hour))
	middle := putCleanupBlob(ctx, t, metadata, objects, []byte("2222"), now.Add(-2*time.Hour))
	newest := putCleanupBlob(ctx, t, metadata, objects, []byte("3333"), now.Add(-time.Hour))

	report, err := cache.NewCleanupService(metadata, objects).CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:   168 * time.Hour,
		MaxBytes:    10,
		TargetBytes: 5,
		DryRun:      true,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("cleanup blobs: %v", err)
	}
	if !report.DryRun || report.DeletedBlobs != 2 || report.BytesDeleted != 8 || report.BytesAfter != 4 {
		t.Fatalf("unexpected dry-run report: %#v", report)
	}
	if report.DeletedDigests[0] != oldest || report.DeletedDigests[1] != middle {
		t.Fatalf("unexpected planned digests: %#v", report.DeletedDigests)
	}
	assertObjectsExist(ctx, t, objects, oldest, middle, newest)
}
func putCleanupBlob(
	ctx context.Context,
	t *testing.T,
	metadata meta.Store,
	objects object.Store,
	body []byte,
	lastAccessAt time.Time,
) string {
	t.Helper()
	digest := testDigestFor(body)
	info, err := objects.Put(ctx, digest, bytes.NewReader(body), object.PutOptions{
		ContentType: distribution.MediaTypeOctetStream,
	})
	if err != nil {
		t.Fatalf("put cleanup object: %v", err)
	}
	_, err = metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       digest,
		Size:         info.Size,
		MediaType:    distribution.MediaTypeOctetStream,
		ObjectKey:    digest,
		LastAccessAt: lastAccessAt,
	})
	if err != nil {
		t.Fatalf("upsert cleanup blob: %v", err)
	}
	return digest
}

func assertCleanupReportCounts(t *testing.T, report *cache.CleanupReport, deleted, recent, protected int) {
	t.Helper()

	if report.DeletedBlobs != deleted || report.RecentBlobs != recent || report.ProtectedBlobs != protected {
		t.Fatalf("unexpected cleanup report: %#v", report)
	}
}

func assertObjectsExist(ctx context.Context, t *testing.T, objects object.Store, digests ...string) {
	t.Helper()

	for _, digest := range digests {
		ok, err := objects.Exists(ctx, digest)
		if err != nil {
			t.Fatalf("object exists %s: %v", digest, err)
		}
		if !ok {
			t.Fatalf("expected object to remain: %s", digest)
		}
	}
}

func assertObjectMissing(ctx context.Context, t *testing.T, objects object.Store, digest string) {
	t.Helper()

	ok, err := objects.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("object exists %s: %v", digest, err)
	}
	if ok {
		t.Fatalf("expected object to be deleted: %s", digest)
	}
}

package cache_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
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
	if report.DeletedBlobs != 1 || report.RecentBlobs != 1 || report.ProtectedBlobs != 1 {
		t.Fatalf("unexpected cleanup report: %#v", report)
	}
	for _, digest := range []string{recentDigest, manifestProtectedDigest} {
		ok, err := objects.Exists(ctx, digest)
		if err != nil {
			t.Fatalf("object exists %s: %v", digest, err)
		}
		if !ok {
			t.Fatalf("expected protected/recent object to remain: %s", digest)
		}
	}
	ok, err := objects.Exists(ctx, repoOnlyDigest)
	if err != nil {
		t.Fatalf("object exists %s: %v", repoOnlyDigest, err)
	}
	if ok {
		t.Fatalf("expected stale repo-only object to be deleted: %s", repoOnlyDigest)
	}
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

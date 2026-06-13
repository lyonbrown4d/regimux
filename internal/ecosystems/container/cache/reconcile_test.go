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

func TestCleanupServiceReconcileBlobsRepairsMissingMetadata(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	metadata, objects := newTestStores(t)
	body := []byte("blob exists without metadata")
	digest := testDigestFor(body)

	info, err := objects.Put(ctx, digest, bytes.NewReader(body), object.PutOptions{
		ContentType: distribution.MediaTypeOctetStream,
	})
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	before := metadataStats(ctx, t, metadata, now)
	if before.BlobBytes != 0 || before.BlobCount != 0 {
		t.Fatalf("expected empty blob metadata before reconcile: %#v", before)
	}

	report := reconcileBlobs(ctx, t, metadata, objects, now)
	assertReconcileReport(t, report, info.Size)

	after := metadataStats(ctx, t, metadata, now)
	if after.BlobCount != 1 || after.BlobBytes != int64(len(body)) {
		t.Fatalf("unexpected blob metadata stats after reconcile: %#v", after)
	}
	blob := reconciledBlob(ctx, t, metadata, digest)
	if blob.Size != int64(len(body)) || blob.MediaType != distribution.MediaTypeOctetStream || blob.ObjectKey != digest {
		t.Fatalf("unexpected reconciled blob metadata: %#v", blob)
	}
}

func reconcileBlobs(ctx context.Context, t *testing.T, metadata meta.Store, objects object.Store, now time.Time) *cache.ReconcileReport {
	t.Helper()
	report, err := cache.NewCleanupService(metadata, objects).ReconcileBlobs(ctx, cache.ReconcileOptions{
		Now: now,
	})
	if err != nil {
		t.Fatalf("reconcile blobs: %v", err)
	}
	return report
}

func assertReconcileReport(t *testing.T, report *cache.ReconcileReport, size int64) {
	t.Helper()
	if report == nil ||
		report.ScannedObjects != 1 ||
		report.MissingMetadata != 1 ||
		report.RepairedMetadata != 1 ||
		report.BytesRepaired != size {
		t.Fatalf("unexpected reconcile report: %#v", report)
	}
}

func reconciledBlob(ctx context.Context, t *testing.T, metadata meta.Store, digest string) *meta.BlobRecord {
	t.Helper()
	blob, ok, err := metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil || !ok {
		t.Fatalf("blob metadata lookup after reconcile: ok=%v err=%v", ok, err)
	}
	return blob
}

func metadataStats(ctx context.Context, t *testing.T, metadata meta.Store, now time.Time) meta.MetadataStats {
	t.Helper()
	stats, err := metadata.MetadataStats(ctx, now)
	if err != nil {
		t.Fatalf("metadata stats: %v", err)
	}
	return stats
}

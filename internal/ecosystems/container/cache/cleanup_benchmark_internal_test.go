package cache

import (
	"context"
	"log/slog"
	"strconv"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

func BenchmarkCleanupServicePlanCapacityReclaim(b *testing.B) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	metadata, objects := newBenchmarkStores(b)
	seedBenchmarkBlobs(ctx, b, metadata, now, 1000)
	service := NewCleanupService(metadata, objects)
	service.logger = slog.New(slog.DiscardHandler)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report, err := service.CleanupBlobs(ctx, benchmarkCleanupOptions(now))
		if err != nil {
			b.Fatalf("cleanup blobs: %v", err)
		}
		if !report.DryRun || !report.CapacityExceeded {
			b.Fatalf("unexpected cleanup report: %#v", report)
		}
	}
}

func BenchmarkCleanupPlanCapacityReclaim(b *testing.B) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	opts := benchmarkCleanupOptions(now)
	baseline := collectionlist.NewList(benchmarkBlobRecords(now, 1000)...)
	protected := collectionset.NewSet[string]()
	service := &CleanupService{logger: slog.New(slog.DiscardHandler)}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		blobs := baseline.Clone()
		report := newCleanupReport(opts, blobs)
		err := service.cleanupBlobRecords(
			ctx,
			opts,
			now.Add(-opts.UnusedFor),
			blobs,
			protected,
			report,
		)
		if err != nil {
			b.Fatalf("plan cleanup: %v", err)
		}
		if !report.CapacityExceeded || report.DeletedBlobs == 0 {
			b.Fatalf("unexpected cleanup report: %#v", report)
		}
	}
}

func benchmarkCleanupOptions(now time.Time) CleanupOptions {
	return CleanupOptions{
		UnusedFor:   168 * time.Hour,
		MaxBytes:    500_000,
		TargetBytes: 250_000,
		DryRun:      true,
		Now:         now,
	}
}

func newBenchmarkStores(b *testing.B) (meta.Store, object.Store) {
	b.Helper()
	metadata, err := meta.OpenSQLite(b.TempDir()+"/regimux.db", nil)
	if err != nil {
		b.Fatalf("open metadata store: %v", err)
	}
	b.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			b.Fatalf("close metadata store: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(b.TempDir())
	if err != nil {
		b.Fatalf("open object store: %v", err)
	}
	return metadata, objects
}

func seedBenchmarkBlobs(
	ctx context.Context,
	b *testing.B,
	metadata meta.Store,
	now time.Time,
	count int,
) {
	b.Helper()
	records := benchmarkBlobRecords(now, count)
	for index := range records {
		if _, err := metadata.UpsertBlob(ctx, records[index]); err != nil {
			b.Fatalf("upsert benchmark blob: %v", err)
		}
	}
}

func benchmarkBlobRecords(now time.Time, count int) []meta.BlobRecord {
	records := make([]meta.BlobRecord, 0, count)
	for i := range count {
		digest := ocidigest.SHA256.FromString(strconv.Itoa(i)).String()
		records = append(records, meta.BlobRecord{
			Digest:       digest,
			Size:         int64(1024 + i%256),
			MediaType:    distribution.MediaTypeOctetStream,
			ObjectKey:    digest,
			LastAccessAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}
	return records
}

package cache_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
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
	service := cache.NewCleanupService(metadata, objects)

	b.ResetTimer()
	for b.Loop() {
		report, err := service.CleanupBlobs(ctx, cache.CleanupOptions{
			UnusedFor:   168 * time.Hour,
			MaxBytes:    500_000,
			TargetBytes: 250_000,
			DryRun:      true,
			Now:         now,
		})
		if err != nil {
			b.Fatalf("cleanup blobs: %v", err)
		}
		if !report.DryRun || !report.CapacityExceeded {
			b.Fatalf("unexpected cleanup report: %#v", report)
		}
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
	objects, err := object.NewMemory("benchmark-objects")
	if err != nil {
		b.Fatalf("open object store: %v", err)
	}
	return metadata, objects
}

func seedBenchmarkBlobs(ctx context.Context, b *testing.B, metadata meta.Store, now time.Time, count int) {
	b.Helper()
	for i := range count {
		digest := ocidigest.SHA256.FromString(strconv.Itoa(i)).String()
		size := int64(1024 + i%256)
		_, err := metadata.UpsertBlob(ctx, meta.BlobRecord{
			Digest:       digest,
			Size:         size,
			MediaType:    distribution.MediaTypeOctetStream,
			ObjectKey:    digest,
			LastAccessAt: now.Add(-time.Duration(i) * time.Minute),
		})
		if err != nil {
			b.Fatalf("upsert benchmark blob: %v", err)
		}
	}
}

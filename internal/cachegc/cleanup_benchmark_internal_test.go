package cachegc

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

func BenchmarkCleanupPlanCapacityReclaim(b *testing.B) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	opts := CleanupOptions{
		UnusedFor:   168 * time.Hour,
		MaxBytes:    500_000,
		TargetBytes: 250_000,
		DryRun:      true,
		Now:         now,
	}
	blobs := benchmarkBlobRecords(now, 1000)
	service := &Service{}
	refs := &referenceSnapshot{protected: set.NewSet[string]()}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report := newReportFromRecords(opts, blobs)
		ordered := orderBlobRecords(slices.Clone(blobs), report.CapacityExceeded)
		if err := service.cleanupCandidates(ctx, opts, now.Add(-opts.UnusedFor), ordered, refs, report); err != nil {
			b.Fatalf("plan cleanup: %v", err)
		}
		if !report.CapacityExceeded || report.DeletedBlobs == 0 {
			b.Fatalf("unexpected cleanup report: %#v", report)
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

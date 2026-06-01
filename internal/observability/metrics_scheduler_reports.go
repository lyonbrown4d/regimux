package observability

import (
	"context"

	"github.com/arcgolabs/observabilityx"
)

type CleanupReportMetrics struct {
	DryRun                 bool
	ScannedBlobs           int
	RecentBlobs            int
	MissingAccessTimeBlobs int
	ProtectedBlobs         int
	EligibleBlobs          int
	DeletedBlobs           int
	MissingObjects         int
	BytesBefore            int64
	BytesAfter             int64
	BytesTarget            int64
	BytesDeleted           int64
	CapacityExceeded       bool
	LimitReached           bool
}

type PrefetchReportMetrics struct {
	ScannedRecords      int
	SkippedRecords      int
	Repositories        int
	SkippedRepositories int
	Candidates          int
	Prefetched          int
	Failed              int
	SkippedCandidates   int
	BytesWarmed         int64
	RetryRequested      bool
	Canceled            bool
}

func (m *Metrics) ObserveCleanupReport(ctx context.Context, report CleanupReportMetrics) {
	if m == nil {
		return
	}
	attrs := []observabilityx.Attribute{observabilityx.String("dry_run", boolLabel(report.DryRun))}
	m.setCleanupBlobGauge(ctx, "scanned", report.ScannedBlobs, attrs)
	m.setCleanupBlobGauge(ctx, "recent", report.RecentBlobs, attrs)
	m.setCleanupBlobGauge(ctx, "missing_access_time", report.MissingAccessTimeBlobs, attrs)
	m.setCleanupBlobGauge(ctx, "protected", report.ProtectedBlobs, attrs)
	m.setCleanupBlobGauge(ctx, "eligible", report.EligibleBlobs, attrs)
	m.setCleanupBlobGauge(ctx, "deleted", report.DeletedBlobs, attrs)
	m.setCleanupBlobGauge(ctx, "missing_objects", report.MissingObjects, attrs)
	m.setCleanupBlobGauge(ctx, "capacity_exceeded", boolInt(report.CapacityExceeded), attrs)
	m.setCleanupBlobGauge(ctx, "limit_reached", boolInt(report.LimitReached), attrs)
	m.setCleanupByteGauge(ctx, "before", report.BytesBefore, attrs)
	m.setCleanupByteGauge(ctx, "after", report.BytesAfter, attrs)
	m.setCleanupByteGauge(ctx, "target", report.BytesTarget, attrs)
	m.setCleanupByteGauge(ctx, "deleted", report.BytesDeleted, attrs)
}

func (m *Metrics) ObservePrefetchReport(ctx context.Context, report PrefetchReportMetrics) {
	if m == nil {
		return
	}
	m.setPrefetchGauge(ctx, "scanned_records", report.ScannedRecords)
	m.setPrefetchGauge(ctx, "skipped_records", report.SkippedRecords)
	m.setPrefetchGauge(ctx, "repositories", report.Repositories)
	m.setPrefetchGauge(ctx, "skipped_repositories", report.SkippedRepositories)
	m.setPrefetchGauge(ctx, "candidates", report.Candidates)
	m.setPrefetchGauge(ctx, "prefetched", report.Prefetched)
	m.setPrefetchGauge(ctx, "failed", report.Failed)
	m.setPrefetchGauge(ctx, "skipped_candidates", report.SkippedCandidates)
	m.setPrefetchGauge(ctx, "retry_requested", boolInt(report.RetryRequested))
	m.setPrefetchGauge(ctx, "canceled", boolInt(report.Canceled))
	m.scheduler.prefetchLastByte.Set(ctx, float64(report.BytesWarmed))
}

func (m *Metrics) setCleanupBlobGauge(ctx context.Context, kind string, value int, attrs []observabilityx.Attribute) {
	m.scheduler.cleanupLastBlobs.Set(ctx, float64(value), appendMetricKind(attrs, kind)...)
}

func (m *Metrics) setCleanupByteGauge(ctx context.Context, kind string, value int64, attrs []observabilityx.Attribute) {
	m.scheduler.cleanupLastBytes.Set(ctx, float64(value), appendMetricKind(attrs, kind)...)
}

func (m *Metrics) setPrefetchGauge(ctx context.Context, kind string, value int) {
	m.scheduler.prefetchLast.Set(ctx, float64(value), observabilityx.String("kind", kind))
}

func appendMetricKind(attrs []observabilityx.Attribute, kind string) []observabilityx.Attribute {
	out := make([]observabilityx.Attribute, 0, len(attrs)+1)
	out = append(out, attrs...)
	out = append(out, observabilityx.String("kind", kind))
	return out
}

package scheduler

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func (r *Runtime) observeJob(ctx context.Context, job, alias string, startedAt time.Time, err error) {
	if r == nil || r.metrics == nil {
		return
	}
	r.metrics.ObserveSchedulerJob(ctx, job, alias, time.Since(startedAt), err)
}

func (r *Runtime) observeCleanupReport(ctx context.Context, report *cache.CleanupReport) {
	if r == nil || r.metrics == nil || report == nil {
		return
	}
	r.metrics.ObserveCleanupReport(ctx, observability.CleanupReportMetrics{
		DryRun:                 report.DryRun,
		ScannedBlobs:           report.ScannedBlobs,
		RecentBlobs:            report.RecentBlobs,
		MissingAccessTimeBlobs: report.MissingAccessTimeBlobs,
		ProtectedBlobs:         report.ProtectedBlobs,
		EligibleBlobs:          report.EligibleBlobs,
		DeletedBlobs:           report.DeletedBlobs,
		MissingObjects:         report.MissingObjects,
		BytesBefore:            report.BytesBefore,
		BytesAfter:             report.BytesAfter,
		BytesTarget:            report.BytesTarget,
		BytesDeleted:           report.BytesDeleted,
		CapacityExceeded:       report.CapacityExceeded,
		LimitReached:           report.LimitReached,
	})
}

func (r *Runtime) observePrefetchReport(ctx context.Context, report *ecosystem.PrefetchReport) {
	if r == nil || r.metrics == nil || report == nil {
		return
	}
	r.metrics.ObservePrefetchReport(ctx, observability.PrefetchReportMetrics{
		ScannedRecords:      report.ScannedRecords,
		SkippedRecords:      report.SkippedRecords,
		Repositories:        report.Repositories,
		SkippedRepositories: report.SkippedRepositories,
		Candidates:          report.Candidates,
		Prefetched:          report.Prefetched,
		Failed:              report.Failed,
		SkippedCandidates:   report.SkippedCandidates,
		BytesWarmed:         report.BytesWarmed,
		RetryRequested:      report.RetryRequested,
		Canceled:            report.Canceled,
	})
}

type endpointHealthObserver interface {
	ObserveEndpointHealth(context.Context, *observability.Metrics)
}

func (r *Runtime) observeEndpointHealth(ctx context.Context) {
	if r == nil || r.metrics == nil {
		return
	}
	for _, runtime := range r.runtimes {
		observer, ok := runtime.(endpointHealthObserver)
		if ok {
			observer.ObserveEndpointHealth(ctx, r.metrics)
		}
	}
}

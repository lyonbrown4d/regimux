package container

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/samber/oops"
)

func (r *Runtime) Cleanup(ctx context.Context) (*ecosystem.CleanupReport, error) {
	if r == nil || r.cleanup == nil {
		return nil, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container cleanup service is not configured")
	}
	if _, err := r.cleanup.ReconcileBlobs(ctx, cache.ReconcileOptions{
		MaxScan: r.cfg.Scheduler.Cleanup.MaxScan,
		DryRun:  r.cfg.Scheduler.Cleanup.DryRun,
	}); err != nil {
		return nil, oops.Wrapf(err, "reconcile container blob metadata")
	}
	report, err := r.cleanup.CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:   r.cfg.Scheduler.Cleanup.UnusedFor,
		MaxDeletes:  r.cfg.Scheduler.Cleanup.MaxDeletes,
		MaxScan:     r.cfg.Scheduler.Cleanup.MaxScan,
		MaxBytes:    r.cfg.Scheduler.Cleanup.MaxBytes,
		TargetBytes: r.cfg.Scheduler.Cleanup.TargetBytes,
		DryRun:      r.cfg.Scheduler.Cleanup.DryRun,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "run container cleanup")
	}
	return containerCleanupReport(report), nil
}

func containerCleanupReport(report *cache.CleanupReport) *ecosystem.CleanupReport {
	if report == nil {
		return nil
	}
	return &ecosystem.CleanupReport{
		Ecosystem:              ecosystem.Container,
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
	}
}

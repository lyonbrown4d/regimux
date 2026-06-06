package container

import (
	"context"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/samber/oops"
)

func (r *Runtime) addCleanupJob(jobs *collectionlist.List[ecosystem.JobSpec]) {
	cfg := r.cfg.Scheduler.Cleanup
	jobs.Add(ecosystem.JobSpec{
		Name:        "regimux." + r.Name() + ".cache.cleanup",
		Kind:        ecosystem.JobCleanup,
		Ecosystem:   r.Name(),
		Tags:        collectionlist.NewList("maintenance", "cleanup", r.Name()),
		Interval:    cfg.Interval,
		Enabled:     cfg.Enabled && cfg.Interval > 0,
		Distributed: cfg.Distributed,
		Run: func(ctx context.Context) (ecosystem.JobRunResult, error) {
			report, err := r.Cleanup(ctx)
			return ecosystem.JobRunResult{CleanupReport: report}, err
		},
	})
}

func (r *Runtime) Cleanup(ctx context.Context) (*ecosystem.CleanupReport, error) {
	if r == nil || r.cleanup == nil {
		return nil, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container cleanup service is not configured")
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

package scheduler

import (
	"context"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/samber/oops"
)

func (r *Runtime) registerCleanup(ctx context.Context, scheduler gocron.Scheduler) error {
	cfg := r.cfg.Scheduler.Cleanup
	if !cfg.Enabled || cfg.Interval <= 0 {
		return nil
	}
	options := []gocron.JobOption{
		gocron.WithName("regimux.cache.cleanup"),
		gocron.WithTags("maintenance", "cleanup"),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	}
	if !cfg.Distributed {
		options = append(options, gocron.WithDisabledDistributedJobLocker(true))
	}
	if _, err := scheduler.NewJob(
		gocron.DurationJob(cfg.Interval),
		gocron.NewTask(r.runCleanup),
		options...,
	); err != nil {
		return oops.Wrapf(err, "register cleanup job")
	}
	return nil
}

func (r *Runtime) registerPrefetch(ctx context.Context, scheduler gocron.Scheduler) error {
	cfg := r.cfg.Scheduler.Prefetch
	if !cfg.Enabled || cfg.Interval <= 0 {
		return nil
	}
	options := []gocron.JobOption{
		gocron.WithName("regimux.prefetch.manifests"),
		gocron.WithTags("maintenance", "prefetch"),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	}
	if !cfg.Distributed {
		options = append(options, gocron.WithDisabledDistributedJobLocker(true))
	}
	if _, err := scheduler.NewJob(
		gocron.DurationJob(cfg.Interval),
		gocron.NewTask(r.runPrefetch),
		options...,
	); err != nil {
		return oops.Wrapf(err, "register prefetch job")
	}
	return nil
}

func (r *Runtime) runCleanup(ctx context.Context) error {
	report, err := r.cleanup.CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:  r.cfg.Scheduler.Cleanup.UnusedFor,
		MaxDeletes: r.cfg.Scheduler.Cleanup.MaxDeletes,
		DryRun:     r.cfg.Scheduler.Cleanup.DryRun,
	})
	if err != nil {
		return oops.Wrapf(err, "run cleanup job")
	}
	r.logger.InfoContext(ctx, "cleanup job completed",
		"dry_run", report.DryRun,
		"scanned_blobs", report.ScannedBlobs,
		"eligible_blobs", report.EligibleBlobs,
		"deleted_blobs", report.DeletedBlobs,
		"bytes_deleted", report.BytesDeleted,
		"limit_reached", report.LimitReached,
	)
	return nil
}

func (r *Runtime) runPrefetch(ctx context.Context) error {
	report, err := r.prefetch.Run(ctx, prefetch.RunOptions{
		MaxRecords:           r.cfg.Scheduler.Prefetch.MaxRecords,
		MinPullCount:         r.cfg.Scheduler.Prefetch.MinPullCount,
		TagsPageSize:         r.cfg.Scheduler.Prefetch.TagsPageSize,
		MaxCandidatesPerRepo: r.cfg.Scheduler.Prefetch.MaxCandidatesPerRepo,
		MaxVersionDistance:   r.cfg.Scheduler.Prefetch.MaxVersionDistance,
		Accept:               r.cfg.Scheduler.Prefetch.Accept,
	})
	if err != nil {
		return oops.Wrapf(err, "run prefetch job")
	}
	r.logger.InfoContext(ctx, "prefetch job completed",
		"scanned_records", report.ScannedRecords,
		"skipped_records", report.SkippedRecords,
		"repositories", report.Repositories,
		"candidates", report.Candidates,
		"prefetched", report.Prefetched,
		"failed", report.Failed,
	)
	return nil
}

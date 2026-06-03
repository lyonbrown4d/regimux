package scheduler

import (
	"context"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
	"golang.org/x/sync/errgroup"
)

func (r *Runtime) registerPrefetch(ctx context.Context, scheduler gocron.Scheduler) error {
	cfg := r.cfg.Scheduler.Prefetch
	if !cfg.Enabled || cfg.Interval <= 0 {
		return nil
	}
	if r.prefetchers().Len() == 0 {
		r.logger.InfoContext(ctx, "prefetch job skipped because no ecosystem exposes prefetch capability")
		return nil
	}
	options := []gocron.JobOption{
		gocron.WithName("regimux.prefetch"),
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

func (r *Runtime) runPrefetch(ctx context.Context) error {
	options := ecosystem.PrefetchOptions{
		MaxRecords:           r.cfg.Scheduler.Prefetch.MaxRecords,
		MinPullCount:         r.cfg.Scheduler.Prefetch.MinPullCount,
		TagsPageSize:         r.cfg.Scheduler.Prefetch.TagsPageSize,
		MaxCandidatesPerRepo: r.cfg.Scheduler.Prefetch.MaxCandidatesPerRepo,
		MaxVersionDistance:   r.cfg.Scheduler.Prefetch.MaxVersionDistance,
		MaxBytes:             r.cfg.Scheduler.Prefetch.MaxBytes,
		MaxTasks:             r.cfg.Scheduler.Prefetch.MaxTasks,
		MaxRepositories:      r.cfg.Scheduler.Prefetch.MaxRepositories,
		FailureBackoff:       r.cfg.Scheduler.Prefetch.FailureBackoff,
		RetryWindow:          r.cfg.Scheduler.Prefetch.RetryWindow,
		Accept:               r.cfg.Scheduler.Prefetch.Accept,
	}
	group, groupCtx := errgroup.WithContext(ctx)
	r.prefetchers().Range(func(_ int, prefetcher ecosystem.Prefetcher) bool {
		jobPrefetcher := prefetcher
		group.Go(func() error {
			return r.runEcosystemPrefetch(groupCtx, jobPrefetcher, options)
		})
		return true
	})
	if err := group.Wait(); err != nil {
		return oops.Wrapf(err, "run ecosystem prefetch jobs")
	}
	return nil
}

func (r *Runtime) runEcosystemPrefetch(ctx context.Context, prefetcher ecosystem.Prefetcher, options ecosystem.PrefetchOptions) error {
	startedAt := time.Now()
	if prefetcher == nil {
		return nil
	}
	report, err := prefetcher.Prefetch(ctx, options)
	if err != nil {
		err = oops.With("ecosystem", prefetcher.Name()).Wrapf(err, "run ecosystem prefetch job")
		r.observeJob(ctx, "prefetch", prefetcher.Name(), startedAt, err)
		return err
	}
	r.logger.InfoContext(ctx, "ecosystem prefetch job completed",
		"ecosystem", prefetcher.Name(),
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"scanned_records", report.ScannedRecords,
		"skipped_records", report.SkippedRecords,
		"repositories", report.Repositories,
		"skipped_repositories", report.SkippedRepositories,
		"candidates", report.Candidates,
		"prefetched", report.Prefetched,
		"failed", report.Failed,
		"skipped_candidates", report.SkippedCandidates,
		"bytes_warmed", report.BytesWarmed,
		"retry_requested", report.RetryRequested,
	)
	r.observePrefetchReport(ctx, report)
	r.observeJob(ctx, "prefetch", prefetcher.Name(), startedAt, nil)
	return nil
}

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
	return r.registerPrefetchJob(
		ctx,
		scheduler,
		cfg.Enabled,
		cfg.Interval,
		cfg.Distributed,
		gocron.WithName("regimux.prefetch"),
		[]string{"maintenance", "prefetch"},
		"register prefetch job",
		"prefetch job skipped because no ecosystem exposes prefetch capability",
		gocron.NewTask(r.runPrefetch),
	)
}

func (r *Runtime) runPrefetch(ctx context.Context) error {
	return r.runEcosystemPrefetchJobs(ctx, r.prefetchOptions(false), "prefetch")
}

func (r *Runtime) registerManifestRefresh(ctx context.Context, scheduler gocron.Scheduler) error {
	cfg := r.cfg.Scheduler.ManifestRefresh
	return r.registerPrefetchJob(
		ctx,
		scheduler,
		cfg.Enabled,
		cfg.Interval,
		cfg.Distributed,
		gocron.WithName("regimux.manifest_refresh"),
		[]string{"maintenance", "manifest-refresh"},
		"register manifest refresh job",
		"manifest refresh job skipped because no ecosystem exposes prefetch capability",
		gocron.NewTask(r.runManifestRefresh),
	)
}

func (r *Runtime) registerPrefetchJob(
	ctx context.Context,
	scheduler gocron.Scheduler,
	enabled bool,
	interval time.Duration,
	distributed bool,
	nameOption gocron.JobOption,
	tags []string,
	errorMessage string,
	skipMessage string,
	task gocron.Task,
) error {
	if !enabled || interval <= 0 {
		return nil
	}
	if r.prefetchers().Len() == 0 {
		r.logger.InfoContext(ctx, skipMessage)
		return nil
	}
	options := []gocron.JobOption{
		nameOption,
		gocron.WithTags(tags...),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	}
	if !distributed {
		options = append(options, gocron.WithDisabledDistributedJobLocker(true))
	}
	if _, err := scheduler.NewJob(gocron.DurationJob(interval), task, options...); err != nil {
		return oops.With("message", errorMessage).Wrap(err)
	}
	return nil
}

func (r *Runtime) runManifestRefresh(ctx context.Context) error {
	return r.runEcosystemPrefetchJobs(ctx, r.prefetchOptions(true), "manifest_refresh")
}

func (r *Runtime) prefetchOptions(manifestOnly bool) ecosystem.PrefetchOptions {
	cfg := r.cfg.Scheduler.Prefetch
	return ecosystem.PrefetchOptions{
		MaxRecords:           cfg.MaxRecords,
		MinPullCount:         cfg.MinPullCount,
		TagsPageSize:         cfg.TagsPageSize,
		MaxCandidatesPerRepo: cfg.MaxCandidatesPerRepo,
		MaxVersionDistance:   cfg.MaxVersionDistance,
		MaxBytes:             cfg.MaxBytes,
		MaxTasks:             cfg.MaxTasks,
		MaxRepositories:      cfg.MaxRepositories,
		FailureBackoff:       cfg.FailureBackoff,
		RetryWindow:          cfg.RetryWindow,
		Accept:               cfg.Accept,
		ManifestOnly:         manifestOnly,
	}
}

func (r *Runtime) runEcosystemPrefetchJobs(
	ctx context.Context,
	options ecosystem.PrefetchOptions, jobType string,
) error {
	group, groupCtx := errgroup.WithContext(ctx)
	r.prefetchers().Range(func(_ int, prefetcher ecosystem.Prefetcher) bool {
		jobPrefetcher := prefetcher
		group.Go(func() error {
			return r.runEcosystemPrefetch(groupCtx, jobPrefetcher, options, jobType)
		})
		return true
	})
	if err := group.Wait(); err != nil {
		return oops.Wrapf(err, "run ecosystem prefetch jobs")
	}
	return nil
}

func (r *Runtime) runEcosystemPrefetch(ctx context.Context, prefetcher ecosystem.Prefetcher, options ecosystem.PrefetchOptions, jobType string) error {
	startedAt := time.Now()
	if prefetcher == nil {
		return nil
	}
	report, err := prefetcher.Prefetch(ctx, options)
	if err != nil {
		err = oops.With("ecosystem", prefetcher.Name()).With("job_type", jobType).Wrapf(err, "run ecosystem prefetch job")
		r.observeJob(ctx, jobType, prefetcher.Name(), startedAt, err)
		return err
	}
	r.logger.InfoContext(ctx, "ecosystem prefetch job completed",
		"ecosystem", prefetcher.Name(),
		"job_type", jobType,
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
	r.observeJob(ctx, jobType, prefetcher.Name(), startedAt, nil)
	return nil
}

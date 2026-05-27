package scheduler

import (
	"context"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
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

func (r *Runtime) registerProbe(ctx context.Context, scheduler gocron.Scheduler) error {
	var registerErr error
	r.cfg.OrderedUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		probeCfg := upstreamCfg.Probe
		if !probeCfg.Enabled || probeCfg.Interval <= 0 {
			return true
		}
		if r.upstream == nil {
			registerErr = oops.In("scheduler").Errorf("upstream probe client is not configured")
			return false
		}

		jobAlias := alias
		options := []gocron.JobOption{
			gocron.WithName("regimux.upstream.probe." + alias),
			gocron.WithTags("maintenance", "probe", alias),
			gocron.WithContext(ctx),
			gocron.WithSingletonMode(gocron.LimitModeReschedule),
			gocron.WithDisabledDistributedJobLocker(true),
			gocron.WithStartAt(gocron.WithStartImmediately()),
		}
		if _, err := scheduler.NewJob(
			gocron.DurationJob(probeCfg.Interval),
			gocron.NewTask(func(ctx context.Context) error {
				return r.runProbe(ctx, jobAlias)
			}),
			options...,
		); err != nil {
			registerErr = oops.Wrapf(err, "register upstream probe job")
			return false
		}
		r.logger.InfoContext(ctx,
			"registered upstream probe job",
			"alias", alias,
			"interval", probeCfg.Interval,
			"timeout", probeCfg.Timeout,
			"cooldown", probeCfg.Cooldown,
		)
		return true
	})
	return registerErr
}

func (r *Runtime) runCleanup(ctx context.Context) error {
	startedAt := time.Now()
	report, err := r.cleanup.CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:  r.cfg.Scheduler.Cleanup.UnusedFor,
		MaxDeletes: r.cfg.Scheduler.Cleanup.MaxDeletes,
		MaxScan:    r.cfg.Scheduler.Cleanup.MaxScan,
		DryRun:     r.cfg.Scheduler.Cleanup.DryRun,
	})
	if err != nil {
		err = oops.Wrapf(err, "run cleanup job")
		r.observeJob(ctx, "cleanup", "", startedAt, err)
		return err
	}
	r.logger.InfoContext(ctx, "cleanup job completed",
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"dry_run", report.DryRun,
		"scanned_blobs", report.ScannedBlobs,
		"eligible_blobs", report.EligibleBlobs,
		"deleted_blobs", report.DeletedBlobs,
		"bytes_deleted", report.BytesDeleted,
		"limit_reached", report.LimitReached,
	)
	r.observeJob(ctx, "cleanup", "", startedAt, nil)
	return nil
}

func (r *Runtime) runProbe(ctx context.Context, alias string) error {
	startedAt := time.Now()
	if r.upstream == nil {
		err := oops.In("scheduler").Errorf("upstream probe client is not configured")
		r.observeJob(ctx, "probe", alias, startedAt, err)
		return err
	}
	if err := r.upstream.ProbeAlias(ctx, alias); err != nil {
		err = oops.Wrapf(err, "run upstream probe job")
		r.observeJob(ctx, "probe", alias, startedAt, err)
		return err
	}
	r.logger.InfoContext(ctx, "upstream probe job completed", "alias", alias, "duration_ms", time.Since(startedAt).Milliseconds())
	r.observeJob(ctx, "probe", alias, startedAt, nil)
	return nil
}

func (r *Runtime) runPrefetch(ctx context.Context) error {
	startedAt := time.Now()
	report, err := r.prefetch.Run(ctx, prefetch.RunOptions{
		MaxRecords:           r.cfg.Scheduler.Prefetch.MaxRecords,
		MinPullCount:         r.cfg.Scheduler.Prefetch.MinPullCount,
		TagsPageSize:         r.cfg.Scheduler.Prefetch.TagsPageSize,
		MaxCandidatesPerRepo: r.cfg.Scheduler.Prefetch.MaxCandidatesPerRepo,
		MaxVersionDistance:   r.cfg.Scheduler.Prefetch.MaxVersionDistance,
		Accept:               r.cfg.Scheduler.Prefetch.Accept,
	})
	if err != nil {
		err = oops.Wrapf(err, "run prefetch job")
		r.observeJob(ctx, "prefetch", "", startedAt, err)
		return err
	}
	r.logger.InfoContext(ctx, "prefetch job completed",
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"scanned_records", report.ScannedRecords,
		"skipped_records", report.SkippedRecords,
		"repositories", report.Repositories,
		"candidates", report.Candidates,
		"prefetched", report.Prefetched,
		"failed", report.Failed,
	)
	r.observeJob(ctx, "prefetch", "", startedAt, nil)
	return nil
}

func (r *Runtime) observeJob(ctx context.Context, job, alias string, startedAt time.Time, err error) {
	if r == nil || r.metrics == nil {
		return
	}
	r.metrics.ObserveSchedulerJob(ctx, job, alias, time.Since(startedAt), err)
}

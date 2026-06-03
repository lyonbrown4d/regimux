package scheduler

import (
	"context"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

const endpointHealthFlushInterval = 2 * time.Second

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

func (r *Runtime) registerEndpointHealthFlush(ctx context.Context, scheduler gocron.Scheduler) error {
	if len(r.endpointHealthFlushers()) == 0 {
		return nil
	}
	options := []gocron.JobOption{
		gocron.WithName("regimux.endpoint_health.flush"),
		gocron.WithTags("maintenance", "endpoint-health"),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithDisabledDistributedJobLocker(true),
	}
	if _, err := scheduler.NewJob(
		gocron.DurationJob(endpointHealthFlushInterval),
		gocron.NewTask(r.runEndpointHealthFlush),
		options...,
	); err != nil {
		return oops.Wrapf(err, "register endpoint health flush job")
	}
	r.logger.InfoContext(ctx, "registered endpoint health flush job", "interval", endpointHealthFlushInterval)
	return nil
}

func (r *Runtime) runCleanup(ctx context.Context) error {
	startedAt := time.Now()
	report, err := r.cleanup.CleanupBlobs(ctx, cache.CleanupOptions{
		UnusedFor:   r.cfg.Scheduler.Cleanup.UnusedFor,
		MaxDeletes:  r.cfg.Scheduler.Cleanup.MaxDeletes,
		MaxScan:     r.cfg.Scheduler.Cleanup.MaxScan,
		MaxBytes:    r.cfg.Scheduler.Cleanup.MaxBytes,
		TargetBytes: r.cfg.Scheduler.Cleanup.TargetBytes,
		DryRun:      r.cfg.Scheduler.Cleanup.DryRun,
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
		"bytes_before", report.BytesBefore,
		"bytes_after", report.BytesAfter,
		"bytes_target", report.BytesTarget,
		"bytes_deleted", report.BytesDeleted,
		"capacity_exceeded", report.CapacityExceeded,
		"limit_reached", report.LimitReached,
	)
	r.observeCleanupReport(ctx, report)
	r.observeJob(ctx, "cleanup", "", startedAt, nil)
	return nil
}

func (r *Runtime) runEndpointHealthFlush(ctx context.Context) error {
	startedAt := time.Now()
	var flushErr error
	for _, flusher := range r.endpointHealthFlushers() {
		if flusher == nil {
			continue
		}
		if err := flusher.FlushEndpointHealth(ctx); err != nil {
			flushErr = multierr.Append(flushErr, oops.With("ecosystem", flusher.Name()).Wrapf(err, "flush endpoint health"))
		}
	}
	if flushErr != nil {
		err := oops.Wrapf(flushErr, "run endpoint health flush job")
		r.observeJob(ctx, "endpoint_health_flush", "", startedAt, err)
		r.observeEndpointHealth(ctx)
		return err
	}
	r.observeEndpointHealth(ctx)
	r.observeJob(ctx, "endpoint_health_flush", "", startedAt, nil)
	return nil
}

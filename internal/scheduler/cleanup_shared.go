package scheduler

import (
	"context"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

const sharedCleanupJobName = "regimux.cache.cleanup"

func (r *Runtime) registerCleanupJob(ctx context.Context, scheduler gocron.Scheduler) error {
	if r.cleanup == nil || !r.cfg.Scheduler.Cleanup.Enabled {
		return nil
	}
	spec := r.sharedCleanupJobSpec()
	if spec.Interval > 0 {
		return r.registerJobSpec(ctx, scheduler, spec)
	}
	cfg := r.cfg.Scheduler.Cleanup
	zone := cfg.Timezone
	if zone == "" {
		zone = "UTC"
	}
	job, err := registerGocronJob(scheduler, gocron.CronJob("CRON_TZ="+zone+" "+cfg.Cron, false),
		func(ctx context.Context) error { return r.runJobSpec(ctx, spec) },
		schedulerJobOptions{name: spec.Name, tags: spec.Tags.Values(), ctx: ctx, distributed: &cfg.Distributed},
	)
	if err != nil {
		return oops.Wrapf(err, "register nightly shared cache cleanup")
	}
	r.cleanupJob = job
	r.logger.InfoContext(ctx, "registered shared cache cleanup", "cron", cfg.Cron, "timezone", zone)
	return nil
}

func (r *Runtime) sharedCleanupJobSpec() ecosystem.JobSpec {
	cfg := r.cfg.Scheduler.Cleanup
	return ecosystem.JobSpec{
		Name:        sharedCleanupJobName,
		Kind:        ecosystem.JobCleanup,
		Ecosystem:   "all",
		Tags:        collectionlist.NewList("maintenance", "cleanup", "all"),
		Enabled:     cfg.Enabled && (cfg.Interval > 0 || strings.TrimSpace(cfg.Cron) != ""),
		Interval:    cfg.Interval,
		Distributed: cfg.Distributed,
		Run: func(ctx context.Context) (ecosystem.JobRunResult, error) {
			report, err := r.runSharedCleanup(ctx)
			return ecosystem.JobRunResult{CleanupReport: report}, err
		},
	}
}

func (r *Runtime) runSharedCleanup(ctx context.Context) (*ecosystem.CleanupReport, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return nil, oops.Wrapf(err, "start shared cache cleanup")
	}
	select {
	case r.cleanupGate <- struct{}{}:
		defer func() { <-r.cleanupGate }()
	default:
		r.logger.InfoContext(ctx, "shared cache cleanup already running")
		return &ecosystem.CleanupReport{Ecosystem: "all", DryRun: r.cfg.Scheduler.Cleanup.DryRun}, nil
	}
	cfg := r.cfg.Scheduler.Cleanup
	ctx = meta.WithCleanupSnapshotLimits(ctx, meta.CleanupSnapshotLimits{
		MaxRecords: cfg.MetadataMaxRecords,
		MaxBytes:   cfg.MetadataMaxBytes,
	})
	now := r.cleanupNow()
	if _, err := r.cleanup.ReconcileBlobs(ctx, cachegc.ReconcileOptions{
		MaxScan: cfg.MaxScan,
		DryRun:  cfg.DryRun,
		Now:     now,
	}); err != nil {
		return nil, oops.Wrapf(err, "reconcile shared cache objects")
	}
	return r.runPackageCleanup(ctx, now, started)
}

func (r *Runtime) runPackageCleanup(ctx context.Context, now, started time.Time) (*ecosystem.CleanupReport, error) {
	cfg := r.cfg.Scheduler.Cleanup
	inventory, err := cachegc.LoadCleanupInventory(ctx, r.metadata)
	if err != nil {
		return nil, oops.Wrapf(err, "load package cleanup inventory")
	}
	opts := ecosystem.CacheCleanupOptions{Now: now, UnusedFor: cfg.UnusedFor, MaxScan: cfg.MaxScan, MaxDeletes: cfg.MaxDeletes, DryRun: cfg.DryRun}
	groups, err := r.planPackageCleanup(ctx, inventory, opts)
	if err != nil {
		return nil, err
	}
	packages, err := r.cleanup.ApplyCleanupPlan(ctx, inventory, groups, opts)
	if err != nil {
		return nil, oops.Wrapf(err, "retire package cache references")
	}
	collector := cachegc.NewCleanupService(packages.Metadata, r.objects)
	report, err := collector.CleanupBlobs(ctx, cachegc.CleanupOptions{
		UnusedFor: cfg.UnusedFor, MaxBytes: cfg.MaxBytes, TargetBytes: cfg.TargetBytes,
		DryRun: cfg.DryRun, Now: now, Candidates: packages.Candidates,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "clean shared dependency cache")
	}
	r.logger.InfoContext(ctx, "shared cache cleanup completed",
		"scope", "all", "duration", time.Since(started), "dry_run", report.DryRun,
		"scanned_blobs", report.ScannedBlobs, "deleted_blobs", report.DeletedBlobs,
		"bytes_deleted", report.BytesDeleted, "limit_reached", report.LimitReached,
		"scanned_packages", packages.ScannedPackages, "evicted_packages", packages.EvictedPackages,
		"retired_references", packages.RetiredReferences,
	)
	result := sharedCleanupReport(report)
	result.ScannedPackages = packages.ScannedPackages
	result.EvictedPackages = packages.EvictedPackages
	result.LimitReached = result.LimitReached || packages.LimitReached
	return result, nil
}

func (r *Runtime) planPackageCleanup(ctx context.Context, inventory ecosystem.CacheCleanupInventory, opts ecosystem.CacheCleanupOptions) ([]ecosystem.CacheCleanupGroup, error) {
	var groups []ecosystem.CacheCleanupGroup
	var planErr error
	r.Capabilities[ecosystem.CacheCleanupPlanner]().Range(func(_ int, planner ecosystem.CacheCleanupPlanner) bool {
		planned, err := planner.PlanCacheCleanup(ctx, inventory, opts)
		if err != nil {
			planErr = oops.With("ecosystem", planner.Name()).Wrapf(err, "plan package cache cleanup")
			return false
		}
		groups = append(groups, planned...)
		return true
	})
	return groups, planErr
}

func sharedCleanupReport(report *cachegc.CleanupReport) *ecosystem.CleanupReport {
	if report == nil {
		return nil
	}
	return &ecosystem.CleanupReport{
		Ecosystem:              "all",
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

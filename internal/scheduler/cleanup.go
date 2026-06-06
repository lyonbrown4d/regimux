package scheduler

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

func (r *Runtime) runCleanup(ctx context.Context) error {
	cleaners := r.cleaners()
	if cleaners.Len() == 0 {
		return oops.In("scheduler").Errorf("cleanup service is not configured")
	}
	var cleanupErr error
	cleaners.Range(func(_ int, cleaner ecosystem.Cleaner) bool {
		cleanupErr = multierr.Append(cleanupErr, r.runCleaner(ctx, cleaner))
		return true
	})
	if cleanupErr != nil {
		return oops.Wrapf(cleanupErr, "run cleanup")
	}
	return nil
}

func (r *Runtime) runCleaner(ctx context.Context, cleaner ecosystem.Cleaner) error {
	startedAt := time.Now()
	report, err := cleaner.Cleanup(ctx)
	if err != nil {
		err = oops.With("ecosystem", cleaner.Name()).Wrapf(err, "run cleanup")
		r.observeJob(ctx, string(ecosystem.JobCleanup), cleaner.Name(), startedAt, err)
		return err
	}
	if r.logger != nil {
		r.logger.InfoContext(ctx,
			"cleanup job completed",
			"ecosystem", cleaner.Name(),
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
	}
	r.observeCleanupReport(ctx, report)
	r.observeJob(ctx, string(ecosystem.JobCleanup), cleaner.Name(), startedAt, nil)
	return nil
}

func (r *Runtime) cleaners() *collectionlist.List[ecosystem.Cleaner] {
	cleaners := collectionlist.NewList[ecosystem.Cleaner]()
	if r == nil || r.runtimes == nil {
		return cleaners
	}
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		cleaner, ok := runtime.(ecosystem.Cleaner)
		if ok {
			cleaners.Add(cleaner)
		}
		return true
	})
	return cleaners
}

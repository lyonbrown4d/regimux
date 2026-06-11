package cache

import (
	"context"
	"errors"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

var errStopReconcileWalk = errors.New("stop blob reconcile walk")

type ReconcileOptions struct {
	DryRun  bool
	MaxScan int
	Now     time.Time
}

type ReconcileReport struct {
	DryRun                bool
	ScannedObjects        int
	ExistingMetadata      int
	MissingMetadata       int
	RepairedMetadata      int
	MissingObjects        int
	BytesRepaired         int64
	LimitReached          bool
	RepairedDigests       []string
	UnsupportedObjectWalk bool
}

func (s *CleanupService) ReconcileBlobs(ctx context.Context, opts ReconcileOptions) (*ReconcileReport, error) {
	if err := s.validateReconcile(ctx, opts); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	now := cleanupNow(opts.Now)
	walker, ok := s.objects.(object.ObjectWalker)
	if !ok {
		return &ReconcileReport{
			DryRun:                opts.DryRun,
			UnsupportedObjectWalk: true,
		}, nil
	}

	report := &ReconcileReport{DryRun: opts.DryRun}
	s.logger.InfoContext(ctx,
		"cache blob metadata reconcile starting",
		"max_scan", opts.MaxScan,
		"dry_run", opts.DryRun,
	)
	err := walker.WalkObjects(ctx, func(info object.Info) error {
		if reconcileScanLimitReached(opts, report) {
			report.LimitReached = true
			return errStopReconcileWalk
		}
		return s.reconcileObject(ctx, now, info, report)
	})
	if errors.Is(err, errStopReconcileWalk) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	s.logger.InfoContext(ctx,
		"cache blob metadata reconcile completed",
		"duration", time.Since(startedAt),
		"scanned_objects", report.ScannedObjects,
		"existing_metadata", report.ExistingMetadata,
		"missing_metadata", report.MissingMetadata,
		"repaired_metadata", report.RepairedMetadata,
		"bytes_repaired", report.BytesRepaired,
		"missing_objects", report.MissingObjects,
		"limit_reached", report.LimitReached,
		"dry_run", report.DryRun,
	)
	return report, nil
}

func (s *CleanupService) validateReconcile(ctx context.Context, opts ReconcileOptions) error {
	if err := validateReconcileContext(ctx); err != nil {
		return err
	}
	if s == nil || s.metadata == nil {
		return errorf("reconcile metadata store is required")
	}
	if s.objects == nil {
		return errorf("reconcile object store is required")
	}
	if opts.MaxScan < 0 {
		return errorf("reconcile scan limit cannot be negative")
	}
	return nil
}

func validateReconcileContext(ctx context.Context) error {
	if ctx == nil {
		return errorf("reconcile context is required")
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "reconcile context")
	}
	return nil
}

func (s *CleanupService) reconcileObject(
	ctx context.Context,
	now time.Time,
	walked object.Info,
	report *ReconcileReport,
) error {
	report.ScannedObjects++
	info, err := s.objects.Stat(ctx, walked.Digest)
	switch {
	case err == nil:
	case errors.Is(err, object.ErrNotFound):
		report.MissingObjects++
		return nil
	default:
		return wrapError(err, "stat blob object for reconcile")
	}

	_, ok, err := s.metadata.Blob(ctx, meta.BlobKey{Digest: info.Digest})
	if err != nil {
		return wrapError(err, "lookup blob metadata for reconcile")
	}
	if ok {
		report.ExistingMetadata++
		return nil
	}

	report.MissingMetadata++
	if report.DryRun {
		return nil
	}
	if _, err := s.metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       info.Digest,
		Size:         info.Size,
		MediaType:    distribution.MediaTypeOctetStream,
		ObjectKey:    info.Digest,
		LastAccessAt: now,
	}); err != nil {
		return wrapError(err, "upsert reconciled blob metadata")
	}
	report.RepairedMetadata++
	report.BytesRepaired += info.Size
	report.RepairedDigests = append(report.RepairedDigests, info.Digest)
	s.logger.DebugContext(ctx, "cache blob metadata reconciled", "digest", info.Digest, "size", info.Size)
	return nil
}

func reconcileScanLimitReached(opts ReconcileOptions, report *ReconcileReport) bool {
	return opts.MaxScan > 0 && report.ScannedObjects >= opts.MaxScan
}

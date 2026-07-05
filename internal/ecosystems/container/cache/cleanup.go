package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type CleanupService struct {
	metadata meta.Store
	objects  object.Store
	logger   *slog.Logger
}

type CleanupOptions struct {
	UnusedFor   time.Duration
	MaxDeletes  int
	MaxScan     int
	MaxBytes    int64
	TargetBytes int64
	DryRun      bool
	Now         time.Time
}

type CleanupReport struct {
	DryRun                 bool
	ScannedBlobs           int
	RecentBlobs            int
	MissingAccessTimeBlobs int
	ProtectedBlobs         int
	EligibleBlobs          int
	DeletedBlobs           int
	MissingObjects         int
	BytesBefore            int64
	BytesAfter             int64
	BytesTarget            int64
	BytesDeleted           int64
	CapacityExceeded       bool
	LimitReached           bool
	DeletedDigests         []string
}

type cleanupSkipReason int

const (
	cleanupEligible cleanupSkipReason = iota
	cleanupMissingAccessTime
	cleanupRecent
	cleanupProtected
)

func NewCleanupService(metadata meta.Store, objects object.Store) *CleanupService {
	return &CleanupService{
		metadata: metadata,
		objects:  objects,
		logger:   slog.Default().With("component", "cache.cleanup"),
	}
}

func (s *CleanupService) CleanupBlobs(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	if err := s.validateCleanup(ctx, opts); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	now := cleanupNow(opts.Now)
	s.logger.InfoContext(ctx,
		"cache cleanup starting",
		"unused_for", opts.UnusedFor,
		"max_scan", opts.MaxScan,
		"max_deletes", opts.MaxDeletes,
		"max_bytes", opts.MaxBytes,
		"target_bytes", opts.TargetBytes,
		"dry_run", opts.DryRun,
	)
	blobs, err := s.metadata.ListBlobs(ctx)
	if err != nil {
		return nil, wrapError(err, "list blob metadata for cleanup")
	}
	protected, err := s.protectedBlobDigests(ctx)
	if err != nil {
		return nil, err
	}

	blobList := collectionlist.NewList(blobs...)
	report := newCleanupReport(opts, blobList)
	if err := s.cleanupBlobRecords(ctx, opts, now.Add(-opts.UnusedFor), blobList, protected, report); err != nil {
		return nil, err
	}
	s.logger.InfoContext(ctx,
		"cache cleanup completed",
		"duration", time.Since(startedAt),
		"scanned_blobs", report.ScannedBlobs,
		"eligible_blobs", report.EligibleBlobs,
		"deleted_blobs", report.DeletedBlobs,
		"bytes_deleted", report.BytesDeleted,
		"bytes_before", report.BytesBefore,
		"bytes_after", report.BytesAfter,
		"missing_objects", report.MissingObjects,
		"limit_reached", report.LimitReached,
		"dry_run", report.DryRun,
	)
	return report, nil
}

func (s *CleanupService) validateCleanup(ctx context.Context, opts CleanupOptions) error {
	if err := validateCleanupContext(ctx); err != nil {
		return err
	}
	if s == nil || s.metadata == nil {
		return errorf("cleanup metadata store is required")
	}
	if s.objects == nil {
		return errorf("cleanup object store is required")
	}
	return validateCleanupOptions(opts)
}

func validateCleanupContext(ctx context.Context) error {
	if ctx == nil {
		return errorf("cleanup context is required")
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "cleanup context")
	}
	return nil
}

func validateCleanupOptions(opts CleanupOptions) error {
	if opts.UnusedFor <= 0 {
		return errorf("cleanup unused duration must be positive")
	}
	if opts.MaxScan < 0 {
		return errorf("cleanup scan limit cannot be negative")
	}
	if opts.MaxBytes < 0 {
		return errorf("cleanup max bytes cannot be negative")
	}
	if opts.TargetBytes < 0 {
		return errorf("cleanup target bytes cannot be negative")
	}
	if opts.MaxBytes > 0 && opts.TargetBytes > opts.MaxBytes {
		return errorf("cleanup target bytes cannot exceed max bytes")
	}
	return nil
}

func cleanupNow(now time.Time) time.Time {
	now = now.UTC()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now
}

func (s *CleanupService) cleanupBlobRecords(
	ctx context.Context,
	opts CleanupOptions,
	cutoff time.Time,
	blobs *collectionlist.List[meta.BlobRecord],
	protected *collectionset.Set[string],
	report *CleanupReport,
) error {
	ordered := cleanupOrderedBlobs(blobs, report.needsCapacityReclaim())
	var stopEarly bool
	var outErr error
	ordered.Range(func(_ int, blob meta.BlobRecord) bool {
		stop, err := s.cleanupBlob(ctx, opts, cutoff, &blob, protected, report)
		if err != nil {
			outErr = err
			return false
		}
		if stop {
			stopEarly = true
			return false
		}
		return true
	})
	if outErr != nil {
		return outErr
	}
	if stopEarly {
		return nil
	}
	if ordered.Len() == 0 {
		return nil
	}
	return nil
}

func (s *CleanupService) cleanupBlob(
	ctx context.Context,
	opts CleanupOptions,
	cutoff time.Time,
	blob *meta.BlobRecord,
	protected *collectionset.Set[string],
	report *CleanupReport,
) (bool, error) {
	if cleanupScanLimitReached(opts, report) {
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, wrapError(err, "cleanup context")
	}
	report.ScannedBlobs++
	reason := classifyCleanupBlob(blob, cutoff, protected)
	capacityReclaim := report.needsCapacityReclaim()
	if reason != cleanupEligible && !cleanupCapacityReclaimCandidate(reason, capacityReclaim) {
		report.recordCleanupSkip(reason)
		return false, nil
	}

	report.EligibleBlobs++
	if cleanupDeleteLimitReached(opts, report) {
		return true, nil
	}
	if opts.DryRun {
		report.planBlobDelete(blob)
		return false, nil
	}
	return false, s.deleteBlobObject(ctx, blob, report)
}

func (s *CleanupService) deleteBlobObject(ctx context.Context, blob *meta.BlobRecord, report *CleanupReport) error {
	info, err := s.objects.Stat(ctx, blob.Digest)
	switch {
	case err == nil:
		report.BytesDeleted += info.Size
	case errors.Is(err, object.ErrNotFound):
		report.MissingObjects++
	default:
		return wrapError(err, "stat blob object for cleanup")
	}

	if err := s.objects.Delete(ctx, blob.Digest); err != nil {
		return wrapError(err, "delete blob object for cleanup")
	}
	if err := s.metadata.DeleteBlob(ctx, meta.BlobKey{Digest: blob.Digest}); err != nil {
		return wrapError(err, "delete blob metadata for cleanup")
	}
	report.DeletedBlobs++
	report.DeletedDigests = append(report.DeletedDigests, blob.Digest)
	report.BytesAfter = cleanupRemainingBytes(report.BytesAfter, blob.Size)
	s.logger.DebugContext(ctx, "cache blob deleted", "digest", blob.Digest, "size", blob.Size)
	return nil
}

package cache

import (
	"context"
	"errors"
	"time"

	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type CleanupService struct {
	metadata meta.Store
	objects  object.Store
}

type CleanupOptions struct {
	UnusedFor  time.Duration
	MaxDeletes int
	MaxScan    int
	DryRun     bool
	Now        time.Time
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
	BytesDeleted           int64
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
	}
}

func (s *CleanupService) CleanupBlobs(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	if err := s.validateCleanup(ctx, opts); err != nil {
		return nil, err
	}
	now := cleanupNow(opts.Now)
	blobs, err := s.metadata.ListBlobs(ctx)
	if err != nil {
		return nil, wrapError(err, "list blob metadata for cleanup")
	}
	protected, err := s.protectedBlobDigests(ctx)
	if err != nil {
		return nil, err
	}

	report := &CleanupReport{
		DryRun: opts.DryRun,
	}
	if err := s.cleanupBlobRecords(ctx, opts, now.Add(-opts.UnusedFor), blobs, protected, report); err != nil {
		return nil, err
	}
	return report, nil
}

func (s *CleanupService) validateCleanup(ctx context.Context, opts CleanupOptions) error {
	if ctx == nil {
		return errorf("cleanup context is required")
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "cleanup context")
	}
	if s == nil || s.metadata == nil {
		return errorf("cleanup metadata store is required")
	}
	if s.objects == nil {
		return errorf("cleanup object store is required")
	}
	if opts.UnusedFor <= 0 {
		return errorf("cleanup unused duration must be positive")
	}
	if opts.MaxScan < 0 {
		return errorf("cleanup scan limit cannot be negative")
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
	blobs []meta.BlobRecord,
	protected *collectionset.Set[string],
	report *CleanupReport,
) error {
	for i := range blobs {
		stop, err := s.cleanupBlob(ctx, opts, cutoff, &blobs[i], protected, report)
		if err != nil || stop {
			return err
		}
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
	if reason := classifyCleanupBlob(blob, cutoff, protected); reason != cleanupEligible {
		report.recordCleanupSkip(reason)
		return false, nil
	}

	report.EligibleBlobs++
	if opts.DryRun {
		return false, nil
	}
	if cleanupDeleteLimitReached(opts, report) {
		return true, nil
	}
	return false, s.deleteBlobObject(ctx, blob, report)
}

func cleanupScanLimitReached(opts CleanupOptions, report *CleanupReport) bool {
	if opts.MaxScan <= 0 || report.ScannedBlobs < opts.MaxScan {
		return false
	}
	report.LimitReached = true
	return true
}

func cleanupDeleteLimitReached(opts CleanupOptions, report *CleanupReport) bool {
	if opts.MaxDeletes <= 0 || report.DeletedBlobs < opts.MaxDeletes {
		return false
	}
	report.LimitReached = true
	return true
}

func classifyCleanupBlob(blob *meta.BlobRecord, cutoff time.Time, protected *collectionset.Set[string]) cleanupSkipReason {
	if blob == nil || blob.LastAccessAt.IsZero() {
		return cleanupMissingAccessTime
	}
	if !blob.LastAccessAt.Before(cutoff) {
		return cleanupRecent
	}
	if protected.Contains(blob.Digest) {
		return cleanupProtected
	}
	return cleanupEligible
}

func (r *CleanupReport) recordCleanupSkip(reason cleanupSkipReason) {
	switch reason {
	case cleanupMissingAccessTime:
		r.MissingAccessTimeBlobs++
	case cleanupRecent:
		r.RecentBlobs++
	case cleanupProtected:
		r.ProtectedBlobs++
	case cleanupEligible:
		return
	}
}

func (s *CleanupService) protectedBlobDigests(ctx context.Context) (*collectionset.Set[string], error) {
	manifests, err := s.metadata.ListManifests(ctx)
	if err != nil {
		return nil, wrapError(err, "list manifest metadata for cleanup")
	}

	protected := collectionset.NewSetWithCapacity[string](len(manifests))
	for i := range manifests {
		manifest := &manifests[i]
		if manifest.Digest != "" {
			protected.Add(manifest.Digest)
		}
		if manifest.ObjectKey != "" {
			protected.Add(manifest.ObjectKey)
		}
	}
	return protected, nil
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
	return nil
}

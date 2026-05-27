package cache

import (
	"context"
	"errors"
	"time"

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

func NewCleanupService(metadata meta.Store, objects object.Store) *CleanupService {
	return &CleanupService{
		metadata: metadata,
		objects:  objects,
	}
}

func (s *CleanupService) CleanupBlobs(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	if ctx == nil {
		return nil, errorf("cleanup context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, wrapError(err, "cleanup context")
	}
	if s == nil || s.metadata == nil {
		return nil, errorf("cleanup metadata store is required")
	}
	if s.objects == nil {
		return nil, errorf("cleanup object store is required")
	}
	if opts.UnusedFor <= 0 {
		return nil, errorf("cleanup unused duration must be positive")
	}
	if opts.MaxScan < 0 {
		return nil, errorf("cleanup scan limit cannot be negative")
	}

	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
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
	cutoff := now.Add(-opts.UnusedFor)
	for _, blob := range blobs {
		if opts.MaxScan > 0 && report.ScannedBlobs >= opts.MaxScan {
			report.LimitReached = true
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, wrapError(err, "cleanup context")
		}
		report.ScannedBlobs++
		if blob.LastAccessAt.IsZero() {
			report.MissingAccessTimeBlobs++
			continue
		}
		if blob.LastAccessAt.After(cutoff) || blob.LastAccessAt.Equal(cutoff) {
			report.RecentBlobs++
			continue
		}
		if _, ok := protected[blob.Digest]; ok {
			report.ProtectedBlobs++
			continue
		}

		report.EligibleBlobs++
		if opts.DryRun {
			continue
		}
		if opts.MaxDeletes > 0 && report.DeletedBlobs >= opts.MaxDeletes {
			report.LimitReached = true
			break
		}
		if err := s.deleteBlobObject(ctx, blob, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func (s *CleanupService) protectedBlobDigests(ctx context.Context) (map[string]struct{}, error) {
	manifests, err := s.metadata.ListManifests(ctx)
	if err != nil {
		return nil, wrapError(err, "list manifest metadata for cleanup")
	}

	protected := make(map[string]struct{}, len(manifests))
	for _, manifest := range manifests {
		if manifest.Digest != "" {
			protected[manifest.Digest] = struct{}{}
		}
		if manifest.ObjectKey != "" {
			protected[manifest.ObjectKey] = struct{}{}
		}
	}
	return protected, nil
}

func (s *CleanupService) deleteBlobObject(ctx context.Context, blob meta.BlobRecord, report *CleanupReport) error {
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

// Package cachegc contains shared metadata-aware cleanup for every ecosystem
// that stores artifacts in the global content-addressable object store.
package cachegc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type Service struct {
	metadata          meta.Store
	objects           object.Store
	invalidatePackage func(context.Context, ecosystem.CacheCleanupGroup) error
}

type CleanupOptions struct {
	UnusedFor       time.Duration
	MaxDeletes      int
	MaxScan         int
	MaxBytes        int64
	TargetBytes     int64
	DryRun          bool
	Now             time.Time
	PruneReferences bool
	// Candidates limits physical cleanup to digests selected by ecosystem plans.
	// A nil map preserves the legacy behavior of scanning all blobs.
	Candidates map[string]bool
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

type skipReason uint8

const (
	reasonEligible skipReason = iota
	reasonMissingTime
	reasonRecent
	reasonProtected
)

func New(metadata meta.Store, objects object.Store, options ...Option) *Service {
	service := &Service{metadata: metadata, objects: objects}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// NewCleanupService retains the established cleanup constructor name.
func NewCleanupService(metadata meta.Store, objects object.Store, options ...Option) *Service {
	return New(metadata, objects, options...)
}

// CleanupBlobs retains the established container cleanup API name.
func (s *Service) CleanupBlobs(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	return s.Cleanup(ctx, opts)
}

func (s *Service) Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupReport, error) {
	if err := validate(ctx, s, opts); err != nil {
		return nil, err
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	snapshot, err := meta.ReadCleanupSnapshot(ctx, s.metadata)
	if err != nil {
		return nil, fmt.Errorf("read metadata for cleanup: %w", err)
	}
	refs, err := s.loadReferencesFromSnapshot(ctx, now.Add(-opts.UnusedFor), opts.PruneReferences, snapshot)
	if err != nil {
		return nil, err
	}
	report := newReportFromRecords(opts, snapshot.Blobs)
	ordered := orderBlobRecords(snapshot.Blobs, report.CapacityExceeded)
	// Keep only the blob rows used by this pass. Reference handling has moved
	// the needed roots and stale rows into refs, so the raw inventory can go.
	snapshot.Manifests = nil
	snapshot.Tags = nil
	snapshot.RepoBlobs = nil
	snapshot.Pulls = nil
	cutoff := now.Add(-opts.UnusedFor)
	if err := s.cleanupCandidates(ctx, opts, cutoff, ordered, refs, report); err != nil {
		return nil, err
	}
	return report, nil
}

func (s *Service) cleanupCandidates(ctx context.Context, opts CleanupOptions, cutoff time.Time, blobs []meta.BlobRecord, refs *referenceSnapshot, report *CleanupReport) error {
	for i := range blobs {
		if opts.Candidates != nil && !opts.Candidates[blobs[i].Digest] {
			continue
		}
		stop, err := s.cleanupBlobCandidate(ctx, opts, cutoff, &blobs[i], refs, report)
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}
	return nil
}

func (s *Service) cleanupBlobCandidate(ctx context.Context, opts CleanupOptions, cutoff time.Time, blob *meta.BlobRecord, refs *referenceSnapshot, report *CleanupReport) (bool, error) {
	if limitReached(opts.MaxScan, report.ScannedBlobs) {
		report.LimitReached = true
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("cleanup context: %w", err)
	}
	report.ScannedBlobs++
	reason := classify(*blob, cutoff, refs.protected, opts.PruneReferences)
	if !candidateEligible(opts, report, reason) {
		recordSkip(report, reason)
		return false, nil
	}
	return s.processEligibleCandidate(ctx, opts, cutoff, blob, refs, report)
}

func candidateEligible(opts CleanupOptions, report *CleanupReport, reason skipReason) bool {
	capacityRequired := report.CapacityExceeded && report.BytesAfter > report.BytesTarget
	capacityCandidate := reason == reasonRecent || (reason == reasonMissingTime && !opts.PruneReferences)
	return reason == reasonEligible || (capacityRequired && capacityCandidate)
}

func (s *Service) processEligibleCandidate(ctx context.Context, opts CleanupOptions, cutoff time.Time, blob *meta.BlobRecord, refs *referenceSnapshot, report *CleanupReport) (bool, error) {
	if opts.MaxDeletes > 0 && report.DeletedBlobs >= opts.MaxDeletes {
		report.LimitReached = true
		return true, nil
	}
	report.EligibleBlobs++
	if opts.DryRun {
		planDelete(report, blob)
		return false, nil
	}
	deleted, err := s.deleteBlob(ctx, opts, cutoff, blob.Digest, refs, report)
	if err != nil {
		return false, err
	}
	if !deleted {
		report.EligibleBlobs--
		return false, nil
	}
	return false, nil
}

func validate(ctx context.Context, s *Service, opts CleanupOptions) error {
	if ctx == nil {
		return errors.New("cleanup context is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cleanup context: %w", err)
	}
	if s == nil || s.metadata == nil {
		return errors.New("cleanup metadata store is required")
	}
	if s.objects == nil {
		return errors.New("cleanup object store is required")
	}
	return validateOptions(opts)
}

func validateOptions(opts CleanupOptions) error {
	if opts.UnusedFor <= 0 {
		return errors.New("cleanup unused duration must be positive")
	}
	if opts.MaxDeletes < 0 || opts.MaxScan < 0 {
		return errors.New("cleanup limits cannot be negative")
	}
	if opts.MaxBytes < 0 || opts.TargetBytes < 0 {
		return errors.New("cleanup byte limits cannot be negative")
	}
	if opts.MaxBytes > 0 && opts.TargetBytes > opts.MaxBytes {
		return errors.New("cleanup target bytes cannot exceed max bytes")
	}
	return nil
}

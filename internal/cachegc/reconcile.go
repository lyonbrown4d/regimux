package cachegc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

var errStopWalk = errors.New("stop object reconciliation walk")

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

func (s *Service) Reconcile(ctx context.Context, opts ReconcileOptions) (*ReconcileReport, error) {
	walker, supported, err := validateReconcile(ctx, s, opts)
	if err != nil {
		return nil, err
	}
	if !supported {
		return &ReconcileReport{DryRun: opts.DryRun, UnsupportedObjectWalk: true}, nil
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	report := &ReconcileReport{DryRun: opts.DryRun}
	err = walker.WalkObjects(ctx, func(walked object.Info) error {
		return s.reconcileObject(ctx, walked, opts, now, report)
	})
	if errors.Is(err, errStopWalk) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return report, nil
}

func validateReconcile(ctx context.Context, s *Service, opts ReconcileOptions) (object.ObjectWalker, bool, error) {
	if ctx == nil {
		return nil, false, errors.New("reconcile context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("reconcile context: %w", err)
	}
	if s == nil || s.metadata == nil {
		return nil, false, errors.New("reconcile metadata store is required")
	}
	if s.objects == nil {
		return nil, false, errors.New("reconcile object store is required")
	}
	if opts.MaxScan < 0 {
		return nil, false, errors.New("reconcile scan limit cannot be negative")
	}
	walker, ok := s.objects.(object.ObjectWalker)
	return walker, ok, nil
}

func (s *Service) reconcileObject(ctx context.Context, walked object.Info, opts ReconcileOptions, now time.Time, report *ReconcileReport) error {
	if opts.MaxScan > 0 && report.ScannedObjects >= opts.MaxScan {
		report.LimitReached = true
		return errStopWalk
	}
	report.ScannedObjects++
	info, err := s.objects.Stat(ctx, walked.Digest)
	if errors.Is(err, object.ErrNotFound) {
		report.MissingObjects++
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat object for reconcile: %w", err)
	}
	_, exists, err := s.metadata.Blob(ctx, meta.BlobKey{Digest: info.Digest})
	if err != nil {
		return fmt.Errorf("lookup blob metadata for reconcile: %w", err)
	}
	if exists {
		report.ExistingMetadata++
		return nil
	}
	return s.repairMissingBlob(ctx, info, opts.DryRun, now, report)
}

func (s *Service) repairMissingBlob(ctx context.Context, info *object.Info, dryRun bool, now time.Time, report *ReconcileReport) error {
	report.MissingMetadata++
	if dryRun {
		return nil
	}
	if _, err := s.metadata.UpsertBlob(ctx, meta.BlobRecord{Digest: info.Digest, Size: info.Size, MediaType: distribution.MediaTypeOctetStream, ObjectKey: info.Digest, LastAccessAt: now}); err != nil {
		return fmt.Errorf("upsert reconciled blob metadata: %w", err)
	}
	report.RepairedMetadata++
	report.BytesRepaired += info.Size
	report.RepairedDigests = append(report.RepairedDigests, info.Digest)
	return nil
}

// ReconcileBlobs retains the established container cleanup API name.
func (s *Service) ReconcileBlobs(ctx context.Context, opts ReconcileOptions) (*ReconcileReport, error) {
	return s.Reconcile(ctx, opts)
}

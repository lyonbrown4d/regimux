package meta

import (
	"context"
	"errors"
	"fmt"
)

const (
	DefaultCleanupSnapshotMaxRecords = 100_000
	DefaultCleanupSnapshotMaxBytes   = 32 << 20
)

var (
	ErrCleanupSnapshotUnsupported = errors.New("metadata store does not support bounded cleanup snapshots")
	ErrCleanupSnapshotLimit       = errors.New("cleanup metadata snapshot limit exceeded")
)

type CleanupSnapshotLimits struct {
	MaxRecords int
	MaxBytes   int64
}

type CleanupSnapshot struct {
	Manifests []ManifestRecord
	Tags      []TagRecord
	Blobs     []BlobRecord
	RepoBlobs []RepoBlobRecord
	Pulls     []PullRecord
}

// CleanupSnapshotReader reads all five cleanup relations with shared limits.
// Implementations must return no snapshot if a page cannot fit within them.
type CleanupSnapshotReader interface {
	ReadCleanupSnapshot(context.Context) (CleanupSnapshot, error)
}

type cleanupSnapshotLimitsContextKey struct{}

// WithCleanupSnapshotLimits applies per-snapshot limits to cleanup reads.
// A fresh budget is created for each complete snapshot, so independent cleanup
// phases do not accumulate charges after their previous snapshot is released.
func WithCleanupSnapshotLimits(ctx context.Context, limits CleanupSnapshotLimits) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, cleanupSnapshotLimitsContextKey{}, normalizeCleanupSnapshotLimits(limits))
}

// ReadCleanupSnapshot is the cleanup-only bounded metadata reader. Stores that
// cannot guarantee keyset paging are rejected instead of falling back to their
// unrestricted List methods.
func ReadCleanupSnapshot(ctx context.Context, store Store) (CleanupSnapshot, error) {
	if ctx == nil {
		return CleanupSnapshot{}, errors.New("cleanup snapshot context is required")
	}
	if err := ctx.Err(); err != nil {
		return CleanupSnapshot{}, fmt.Errorf("read cleanup metadata snapshot: %w", err)
	}
	if store == nil {
		return CleanupSnapshot{}, errors.New("cleanup snapshot metadata store is required")
	}
	reader, ok := store.(CleanupSnapshotReader)
	if !ok {
		return CleanupSnapshot{}, ErrCleanupSnapshotUnsupported
	}
	snapshot, err := reader.ReadCleanupSnapshot(ctx)
	if err != nil {
		return CleanupSnapshot{}, wrapError(err, "read cleanup metadata snapshot")
	}
	if err := ctx.Err(); err != nil {
		return CleanupSnapshot{}, fmt.Errorf("read cleanup metadata snapshot: %w", err)
	}
	return snapshot, nil
}

func cleanupSnapshotLimits(ctx context.Context) CleanupSnapshotLimits {
	if ctx != nil {
		if limits, ok := ctx.Value(cleanupSnapshotLimitsContextKey{}).(CleanupSnapshotLimits); ok {
			return normalizeCleanupSnapshotLimits(limits)
		}
	}
	return defaultCleanupSnapshotLimits()
}

// CleanupSnapshotLimitsFromContext returns the effective cleanup budget.
// The limits also bound detailed reconciliation output so a streamed walk does
// not accumulate an unbounded list of repaired digests.
func CleanupSnapshotLimitsFromContext(ctx context.Context) CleanupSnapshotLimits {
	return cleanupSnapshotLimits(ctx)
}

func defaultCleanupSnapshotLimits() CleanupSnapshotLimits {
	return CleanupSnapshotLimits{
		MaxRecords: DefaultCleanupSnapshotMaxRecords,
		MaxBytes:   DefaultCleanupSnapshotMaxBytes,
	}
}

func normalizeCleanupSnapshotLimits(limits CleanupSnapshotLimits) CleanupSnapshotLimits {
	defaults := defaultCleanupSnapshotLimits()
	if limits.MaxRecords <= 0 {
		limits.MaxRecords = defaults.MaxRecords
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = defaults.MaxBytes
	}
	return limits
}

type cleanupSnapshotBudget struct {
	limits  CleanupSnapshotLimits
	records int
	bytes   int64
}

func (b *cleanupSnapshotBudget) reserve(relation string, records int, bytes int64) error {
	if records < 0 || bytes < 0 || records > b.limits.MaxRecords-b.records || bytes > b.limits.MaxBytes-b.bytes {
		return fmt.Errorf("%w: relation=%s records=%d/%d bytes=%d/%d", ErrCleanupSnapshotLimit,
			relation, b.records+records, b.limits.MaxRecords, b.bytes+bytes, b.limits.MaxBytes)
	}
	b.records += records
	b.bytes += bytes
	return nil
}

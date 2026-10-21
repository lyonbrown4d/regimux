package meta

import (
	"context"
	"errors"
	"fmt"
	"slices"

	collectionlist "github.com/arcgolabs/collectionx/list"
)

var ErrPrefetchPullsUnsupported = errors.New("metadata store does not support bounded prefetch pull reads")

// PrefetchPullsReader reads the pull relation with a bounded metadata snapshot.
type PrefetchPullsReader interface {
	ReadPrefetchPulls(context.Context, bool) (*collectionlist.List[PullRecord], error)
}

// ReadPrefetchPulls reads pull records without falling back to an unrestricted list.
func ReadPrefetchPulls(
	ctx context.Context,
	store PullRepository,
	recentFirst bool,
) (*collectionlist.List[PullRecord], error) {
	if ctx == nil {
		return nil, errors.New("prefetch pull context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read prefetch pulls: %w", err)
	}
	if store == nil {
		return nil, errors.New("prefetch pull metadata store is required")
	}
	reader, ok := store.(PrefetchPullsReader)
	if !ok {
		return nil, ErrPrefetchPullsUnsupported
	}
	records, err := reader.ReadPrefetchPulls(ctx, recentFirst)
	if err != nil {
		return nil, wrapError(err, "read prefetch pulls")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read prefetch pulls: %w", err)
	}
	return records, nil
}

func (s *SQLStore) ReadPrefetchPulls(
	ctx context.Context,
	recentFirst bool,
) (*collectionlist.List[PullRecord], error) {
	if err := requireMetadataContext(ctx, "read prefetch pulls"); err != nil {
		return nil, err
	}
	budget := cleanupSnapshotBudget{limits: cleanupSnapshotLimits(ctx)}
	records, err := readCleanupPages(ctx, &budget, "prefetch pulls", s.listCleanupPullPage,
		func(row pullRow) int64 { return row.ID }, pullRowBytes, s.mapper.PullRowToRecord)
	if err != nil {
		return nil, err
	}
	if recentFirst {
		slices.SortFunc(records, compareRecentPullRecords)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read prefetch pulls: %w", err)
	}
	return collectionlist.NewList(records...), nil
}

func compareRecentPullRecords(left, right PullRecord) int {
	if comparison := right.LastPullAt.Compare(left.LastPullAt); comparison != 0 {
		return comparison
	}
	if comparison := right.LastPolicyDeniedAt.Compare(left.LastPolicyDeniedAt); comparison != 0 {
		return comparison
	}
	if comparison := right.UpdatedAt.Compare(left.UpdatedAt); comparison != 0 {
		return comparison
	}
	if left.ID > right.ID {
		return -1
	}
	if left.ID < right.ID {
		return 1
	}
	return 0
}

var _ PrefetchPullsReader = (*SQLStore)(nil)

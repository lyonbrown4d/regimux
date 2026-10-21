package meta

import (
	"context"
	"errors"
	"fmt"

	"github.com/arcgolabs/dbx/repository"
)

const cleanupSnapshotPageSize = 256

func (s *SQLStore) ReadCleanupSnapshot(ctx context.Context) (CleanupSnapshot, error) {
	if err := requireMetadataContext(ctx, "read cleanup metadata snapshot"); err != nil {
		return CleanupSnapshot{}, err
	}
	budget := cleanupSnapshotBudget{limits: cleanupSnapshotLimits(ctx)}
	var snapshot CleanupSnapshot
	var err error
	if snapshot.Manifests, err = readCleanupPages(ctx, &budget, "manifests", s.listCleanupManifestPage,
		func(row manifestRow) int64 { return row.ID }, manifestRowBytes, s.mapper.ManifestRowToRecord); err != nil {
		return CleanupSnapshot{}, err
	}
	if snapshot.Tags, err = readCleanupPages(ctx, &budget, "tags", s.listCleanupTagPage,
		func(row tagRow) int64 { return row.ID }, tagRowBytes, s.mapper.TagRowToRecord); err != nil {
		return CleanupSnapshot{}, err
	}
	if snapshot.Blobs, err = readCleanupPages(ctx, &budget, "blobs", s.listCleanupBlobPage,
		func(row blobRow) int64 { return row.ID }, blobRowBytes, func(row blobRow) (*BlobRecord, error) {
			record := blobRecordFromRow(row)
			return &record, nil
		}); err != nil {
		return CleanupSnapshot{}, err
	}
	if snapshot.RepoBlobs, err = readCleanupPages(ctx, &budget, "repository blobs", s.listCleanupRepoBlobPage,
		func(row repoBlobRow) int64 { return row.ID }, repoBlobRowBytes, s.mapper.RepoBlobRowToRecord); err != nil {
		return CleanupSnapshot{}, err
	}
	if snapshot.Pulls, err = readCleanupPages(ctx, &budget, "pulls", s.listCleanupPullPage,
		func(row pullRow) int64 { return row.ID }, pullRowBytes, s.mapper.PullRowToRecord); err != nil {
		return CleanupSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return CleanupSnapshot{}, fmt.Errorf("read cleanup metadata snapshot: %w", err)
	}
	return snapshot, nil
}

func (s *SQLStore) listCleanupManifestPage(ctx context.Context, afterID int64) (rowCollection[manifestRow], error) {
	rows, err := repository.Query(s.manifest).
		Where(sqlManifestRows.ID.Gt(afterID)).
		OrderBy(sqlManifestRows.ID.Asc()).
		Limit(cleanupSnapshotPageSize).
		List(ctx)
	if err != nil {
		return nil, wrapError(err, "list cleanup manifest page")
	}
	return rows, nil
}

func (s *SQLStore) listCleanupTagPage(ctx context.Context, afterID int64) (rowCollection[tagRow], error) {
	rows, err := repository.Query(s.tags).
		Where(sqlTagRows.ID.Gt(afterID)).
		OrderBy(sqlTagRows.ID.Asc()).
		Limit(cleanupSnapshotPageSize).
		List(ctx)
	if err != nil {
		return nil, wrapError(err, "list cleanup tag page")
	}
	return rows, nil
}

func (s *SQLStore) listCleanupBlobPage(ctx context.Context, afterID int64) (rowCollection[blobRow], error) {
	rows, err := repository.Query(s.blobs).
		Where(sqlBlobRows.ID.Gt(afterID)).
		OrderBy(sqlBlobRows.ID.Asc()).
		Limit(cleanupSnapshotPageSize).
		List(ctx)
	if err != nil {
		return nil, wrapError(err, "list cleanup blob page")
	}
	return rows, nil
}

func (s *SQLStore) listCleanupRepoBlobPage(ctx context.Context, afterID int64) (rowCollection[repoBlobRow], error) {
	rows, err := repository.Query(s.repoBlobs).
		Where(sqlRepoBlobRows.ID.Gt(afterID)).
		OrderBy(sqlRepoBlobRows.ID.Asc()).
		Limit(cleanupSnapshotPageSize).
		List(ctx)
	if err != nil {
		return nil, wrapError(err, "list cleanup repository blob page")
	}
	return rows, nil
}

func (s *SQLStore) listCleanupPullPage(ctx context.Context, afterID int64) (rowCollection[pullRow], error) {
	rows, err := repository.Query(s.pulls).
		Where(sqlPullRows.ID.Gt(afterID)).
		OrderBy(sqlPullRows.ID.Asc()).
		Limit(cleanupSnapshotPageSize).
		List(ctx)
	if err != nil {
		return nil, wrapError(err, "list cleanup pull page")
	}
	return rows, nil
}

func readCleanupPages[T any, R any](
	ctx context.Context,
	budget *cleanupSnapshotBudget,
	relation string,
	fetch func(context.Context, int64) (rowCollection[T], error),
	rowID func(T) int64,
	rowBytes func(T) int64,
	mapRow func(T) (*R, error),
) ([]R, error) {
	var records []R
	var cursor int64
	for {
		rows, err := fetchCleanupPage(ctx, relation, fetch, cursor)
		if err != nil {
			return nil, err
		}
		if rows == nil || rows.Len() == 0 {
			return records, nil
		}
		page, nextCursor, err := mapCleanupPage(ctx, budget, relation, rows, cursor, rowID, rowBytes, mapRow)
		if err != nil {
			return nil, err
		}
		records = append(records, page...)
		if rows.Len() < cleanupSnapshotPageSize {
			return records, nil
		}
		cursor = nextCursor
	}
}

func fetchCleanupPage[T any](ctx context.Context, relation string, fetch func(context.Context, int64) (rowCollection[T], error), cursor int64) (rowCollection[T], error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read cleanup %s page: %w", relation, err)
	}
	rows, err := fetch(ctx, cursor)
	if err != nil {
		return nil, wrapError(err, "read cleanup %s page", relation)
	}
	return rows, nil
}

func mapCleanupPage[T any, R any](
	ctx context.Context,
	budget *cleanupSnapshotBudget,
	relation string,
	rows rowCollection[T],
	cursor int64,
	rowID func(T) int64,
	rowBytes func(T) int64,
	mapRow func(T) (*R, error),
) ([]R, int64, error) {
	pageCount := rows.Len()
	pageBytes := int64(0)
	nextCursor := cursor
	scannedRows := 0
	var pageErr error
	rows.Range(func(_ int, row T) bool {
		scannedRows++
		id := rowID(row)
		if id <= nextCursor {
			pageErr = fmt.Errorf("cleanup %s page has non-increasing id %d after %d", relation, id, nextCursor)
			return false
		}
		nextCursor = id
		pageBytes += rowBytes(row)
		return true
	})
	if pageErr != nil {
		return nil, cursor, pageErr
	}
	if scannedRows != pageCount {
		return nil, cursor, fmt.Errorf("cleanup %s page yielded %d rows while reporting %d", relation, scannedRows, pageCount)
	}
	if err := budget.reserve(relation, pageCount, pageBytes); err != nil {
		return nil, cursor, err
	}
	if err := ctx.Err(); err != nil {
		return nil, cursor, fmt.Errorf("map cleanup %s page: %w", relation, err)
	}
	page, err := mapCleanupRecords(relation, rows, pageCount, mapRow)
	if err != nil {
		return nil, cursor, err
	}
	return page, nextCursor, nil
}

func mapCleanupRecords[T any, R any](relation string, rows rowCollection[T], pageCount int, mapRow func(T) (*R, error)) ([]R, error) {
	page := make([]R, 0, pageCount)
	mappedRows := 0
	var mapErr error
	rows.Range(func(_ int, row T) bool {
		record, err := mapRow(row)
		if err != nil {
			mapErr = err
			return false
		}
		if record == nil {
			mapErr = errors.New("mapper returned nil record")
			return false
		}
		page = append(page, *record)
		mappedRows++
		return true
	})
	if mapErr == nil && mappedRows != pageCount {
		mapErr = fmt.Errorf("mapped %d rows while reporting %d", mappedRows, pageCount)
	}
	if mapErr != nil {
		return nil, wrapError(mapErr, "map cleanup %s page", relation)
	}
	return page, nil
}

func manifestRowBytes(row manifestRow) int64 {
	return cleanupStringBytes(row.Key, row.Alias, row.Repository, row.Reference, row.AcceptKey,
		row.Digest, row.MediaType, row.ObjectKey, row.Headers) + 5*8
}

func tagRowBytes(row tagRow) int64 {
	return cleanupStringBytes(row.Key, row.Alias, row.Repository, row.Reference, row.Digest) + 4*8
}

func blobRowBytes(row blobRow) int64 {
	return cleanupStringBytes(row.Digest, row.MediaType, row.ObjectKey) + 5*8
}

func repoBlobRowBytes(row repoBlobRow) int64 {
	return cleanupStringBytes(row.Key, row.Alias, row.Repository, row.Digest, row.SourceManifest) + 5*8
}

func pullRowBytes(row pullRow) int64 {
	return cleanupStringBytes(row.Key, row.Alias, row.Repository, row.Reference) + 7*8
}

func cleanupStringBytes(values ...string) int64 {
	var total int64
	for _, value := range values {
		total += int64(len(value))
	}
	return total
}

var _ CleanupSnapshotReader = (*SQLStore)(nil)

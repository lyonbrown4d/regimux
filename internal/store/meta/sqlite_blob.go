package meta

import (
	"context"
	"errors"
	"time"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLiteStore) Blob(ctx context.Context, key BlobKey) (*BlobRecord, bool, error) {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.blobs, sqliteBlobRows.Digest).Get(ctx, key.Digest)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get blob metadata")
	}
	return blobRowToRecord(row), true, nil
}

func (s *SQLiteStore) UpsertBlob(ctx context.Context, record BlobRecord) (*BlobRecord, error) {
	key, record, err := normalizeBlobRecord(record)
	if err != nil {
		return nil, err
	}
	now := sqliteNow()
	existing, ok, err := s.Blob(ctx, key)
	if err != nil {
		return nil, err
	}
	if ok {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	row := blobRecordToRow(record)
	if err := s.writeBlobRow(ctx, &record, row); err != nil {
		return nil, err
	}
	if err := s.refreshRepositoriesForBlob(ctx, key.Digest, record.UpdatedAt); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLiteStore) writeBlobRow(ctx context.Context, record *BlobRecord, row blobRow) error {
	if record.ID != 0 {
		if err := s.updateBlobRow(ctx, row); err != nil {
			return err
		}
		return nil
	}
	if err := s.blobs.Create(ctx, &row); err != nil {
		return wrapError(err, "upsert blob metadata")
	}
	record.ID = row.ID
	return nil
}

func (s *SQLiteStore) DeleteBlob(ctx context.Context, key BlobKey) error {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.blobs, sqliteBlobRows.Digest).Delete(ctx, key.Digest)
	if err != nil {
		return wrapError(err, "delete blob metadata")
	}
	if err := s.refreshRepositoriesForBlob(ctx, key.Digest, sqliteNow()); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) GetBlob(ctx context.Context, digest string) (*BlobRecord, bool, error) {
	return s.Blob(ctx, BlobKey{Digest: digest})
}

func (s *SQLiteStore) PutBlob(ctx context.Context, record BlobRecord) error {
	if _, err := s.UpsertBlob(ctx, record); err != nil {
		return wrapError(err, "put blob metadata")
	}
	return nil
}

func (s *SQLiteStore) ListBlobs(ctx context.Context, opts ...BlobListOption) ([]BlobRecord, error) {
	options := blobListOptions(opts...)
	query := repository.Query(s.blobs)
	switch options.Order {
	case BlobListDefault:
	case BlobListRecentFirst:
		query = query.OrderBy(
			sqliteBlobRows.LastAccessAt.Desc(),
			sqliteBlobRows.UpdatedAt.Desc(),
			sqliteBlobRows.CreatedAt.Desc(),
			sqliteBlobRows.ID.Desc(),
		)
	case BlobListLargestFirst:
		query = query.OrderBy(
			sqliteBlobRows.Size.Desc(),
			sqliteBlobRows.LastAccessAt.Desc(),
			sqliteBlobRows.UpdatedAt.Desc(),
			sqliteBlobRows.CreatedAt.Desc(),
			sqliteBlobRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list blob metadata")
	}
	return blobRowsToRecords(rows), nil
}

func blobRowsToRecords(rows interface {
	Len() int
	Range(func(int, blobRow) bool)
}) []BlobRecord {
	records := make([]BlobRecord, 0, rows.Len())
	rows.Range(func(_ int, row blobRow) bool {
		records = append(records, *blobRowToRecord(row))
		return true
	})
	return records
}

func (s *SQLiteStore) RepoBlob(ctx context.Context, key RepoBlobKey) (*RepoBlobRecord, bool, error) {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.repoBlobs, sqliteRepoBlobRows.Key).Get(ctx, key.String())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get repository blob metadata")
	}
	return repoBlobRowToRecord(row), true, nil
}

func (s *SQLiteStore) UpsertRepoBlob(ctx context.Context, record RepoBlobRecord) (*RepoBlobRecord, error) {
	key, record, err := normalizeRepoBlobRecord(record)
	if err != nil {
		return nil, err
	}
	now := sqliteNow()
	existing, ok, err := s.RepoBlob(ctx, key)
	if err != nil {
		return nil, err
	}
	record = preserveRepoBlobTimes(record, existing, ok, now)
	row := repoBlobRecordToRow(record)
	if err := s.writeRepoBlobRow(ctx, &record, row); err != nil {
		return nil, err
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, record.UpdatedAt); err != nil {
		return nil, err
	}
	return &record, nil
}

func preserveRepoBlobTimes(record RepoBlobRecord, existing *RepoBlobRecord, ok bool, now time.Time) RepoBlobRecord {
	if ok {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
		if record.LastVerifiedAt.IsZero() {
			record.LastVerifiedAt = existing.LastVerifiedAt
		}
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if record.LastAccessAt.IsZero() {
		record.LastAccessAt = now
	}
	if record.LastVerifiedAt.IsZero() {
		record.LastVerifiedAt = now
	}
	return record
}

func (s *SQLiteStore) writeRepoBlobRow(ctx context.Context, record *RepoBlobRecord, row repoBlobRow) error {
	if record.ID != 0 {
		if err := s.updateRepoBlobRow(ctx, row); err != nil {
			return err
		}
		return nil
	}
	if err := s.repoBlobs.Create(ctx, &row); err != nil {
		return wrapError(err, "upsert repository blob metadata")
	}
	record.ID = row.ID
	return nil
}

func (s *SQLiteStore) DeleteRepoBlob(ctx context.Context, key RepoBlobKey) error {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.repoBlobs, sqliteRepoBlobRows.Key).Delete(ctx, key.String())
	if err != nil {
		return wrapError(err, "delete repository blob metadata")
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, sqliteNow()); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) ListRepoBlobs(ctx context.Context, opts ...RepoBlobListOption) ([]RepoBlobRecord, error) {
	options := repoBlobListOptions(opts...)
	query := repository.Query(s.repoBlobs)
	if options.RecentFirst {
		query = query.OrderBy(
			sqliteRepoBlobRows.LastAccessAt.Desc(),
			sqliteRepoBlobRows.LastVerifiedAt.Desc(),
			sqliteRepoBlobRows.UpdatedAt.Desc(),
			sqliteRepoBlobRows.CreatedAt.Desc(),
			sqliteRepoBlobRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list repository blob metadata")
	}
	return repoBlobRowsToRecords(rows), nil
}

func repoBlobRowsToRecords(rows interface {
	Len() int
	Range(func(int, repoBlobRow) bool)
}) []RepoBlobRecord {
	records := make([]RepoBlobRecord, 0, rows.Len())
	rows.Range(func(_ int, row repoBlobRow) bool {
		records = append(records, *repoBlobRowToRecord(row))
		return true
	})
	return records
}

func (s *SQLiteStore) updateBlobRow(ctx context.Context, row blobRow) error {
	_, err := repository.By(s.blobs, sqliteBlobRows.Digest).Update(ctx, row.Digest,
		sqliteBlobRows.Size.Set(row.Size),
		sqliteBlobRows.MediaType.Set(row.MediaType),
		sqliteBlobRows.ObjectKey.Set(row.ObjectKey),
		sqliteBlobRows.CreatedAt.Set(row.CreatedAt),
		sqliteBlobRows.UpdatedAt.Set(row.UpdatedAt),
		sqliteBlobRows.LastAccessAt.Set(row.LastAccessAt),
	)
	if err != nil {
		return wrapError(err, "upsert blob metadata")
	}
	return nil
}

func (s *SQLiteStore) updateRepoBlobRow(ctx context.Context, row repoBlobRow) error {
	_, err := repository.By(s.repoBlobs, sqliteRepoBlobRows.Key).Update(ctx, row.Key,
		sqliteRepoBlobRows.Alias.Set(row.Alias),
		sqliteRepoBlobRows.Repository.Set(row.Repository),
		sqliteRepoBlobRows.Digest.Set(row.Digest),
		sqliteRepoBlobRows.SourceManifest.Set(row.SourceManifest),
		sqliteRepoBlobRows.CreatedAt.Set(row.CreatedAt),
		sqliteRepoBlobRows.UpdatedAt.Set(row.UpdatedAt),
		sqliteRepoBlobRows.LastAccessAt.Set(row.LastAccessAt),
		sqliteRepoBlobRows.LastVerifiedAt.Set(row.LastVerifiedAt),
	)
	if err != nil {
		return wrapError(err, "upsert repository blob metadata")
	}
	return nil
}

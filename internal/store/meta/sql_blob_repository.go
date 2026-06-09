package meta

import (
	"context"
	"errors"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) Blob(ctx context.Context, key BlobKey) (*BlobRecord, bool, error) {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.blobs, sqlBlobRows.Digest).Get(ctx, key.Digest)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get blob metadata")
	}
	record, err := s.mapper.BlobRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) UpsertBlob(ctx context.Context, record BlobRecord) (*BlobRecord, error) {
	key, record, err := normalizeBlobRecord(record)
	if err != nil {
		return nil, err
	}
	now := metadataNow()
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
	row, err := s.mapper.BlobRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.writeBlobRow(ctx, &record, row); err != nil {
		return nil, err
	}
	if err := s.refreshRepositoriesForBlob(ctx, key.Digest, record.UpdatedAt); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLStore) writeBlobRow(ctx context.Context, record *BlobRecord, row blobRow) error {
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

func (s *SQLStore) DeleteBlob(ctx context.Context, key BlobKey) error {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.blobs, sqlBlobRows.Digest).Delete(ctx, key.Digest)
	if err != nil {
		return wrapError(err, "delete blob metadata")
	}
	if err := s.refreshRepositoriesForBlob(ctx, key.Digest, metadataNow()); err != nil {
		return err
	}
	return nil
}

func (s *SQLStore) GetBlob(ctx context.Context, digest string) (*BlobRecord, bool, error) {
	return s.Blob(ctx, BlobKey{Digest: digest})
}

func (s *SQLStore) PutBlob(ctx context.Context, record BlobRecord) error {
	if _, err := s.UpsertBlob(ctx, record); err != nil {
		return wrapError(err, "put blob metadata")
	}
	return nil
}

func (s *SQLStore) ListBlobs(ctx context.Context, opts ...BlobListOption) ([]BlobRecord, error) {
	options := blobListOptions(opts...)
	query := repository.Query(s.blobs)
	switch options.Order {
	case BlobListDefault:
	case BlobListRecentFirst:
		query = query.OrderBy(
			sqlBlobRows.LastAccessAt.Desc(),
			sqlBlobRows.UpdatedAt.Desc(),
			sqlBlobRows.CreatedAt.Desc(),
			sqlBlobRows.ID.Desc(),
		)
	case BlobListLargestFirst:
		query = query.OrderBy(
			sqlBlobRows.Size.Desc(),
			sqlBlobRows.LastAccessAt.Desc(),
			sqlBlobRows.UpdatedAt.Desc(),
			sqlBlobRows.CreatedAt.Desc(),
			sqlBlobRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list blob metadata")
	}
	return s.blobRowsToRecords(rows)
}

func (s *SQLStore) blobRowsToRecords(rows interface {
	Values() []blobRow
}) ([]BlobRecord, error) {
	return mapRows(rows, s.mapper.BlobRowToRecord)
}

func (s *SQLStore) updateBlobRow(ctx context.Context, row blobRow) error {
	return patchRowByKey(ctx, s.blobs, sqlBlobRows.Digest, row.Digest, "upsert blob metadata",
		sqlBlobRows.Size.Set(row.Size),
		sqlBlobRows.MediaType.Set(row.MediaType),
		sqlBlobRows.ObjectKey.Set(row.ObjectKey),
		sqlBlobRows.CreatedAt.Set(row.CreatedAt),
		sqlBlobRows.UpdatedAt.Set(row.UpdatedAt),
		sqlBlobRows.LastAccessAt.Set(row.LastAccessAt),
	)
}

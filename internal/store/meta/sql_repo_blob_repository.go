package meta

import (
	"context"
	"errors"
	"time"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) RepoBlob(ctx context.Context, key RepoBlobKey) (*RepoBlobRecord, bool, error) {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	row, err := repository.By(s.repoBlobs, sqlRepoBlobRows.Key).Get(ctx, key.String())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get repository blob metadata")
	}
	record, err := s.mapper.RepoBlobRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) UpsertRepoBlob(ctx context.Context, record RepoBlobRecord) (*RepoBlobRecord, error) {
	key, record, err := normalizeRepoBlobRecord(record)
	if err != nil {
		return nil, err
	}
	now := metadataNow()
	existing, ok, err := s.RepoBlob(ctx, key)
	if err != nil {
		return nil, err
	}
	record = preserveRepoBlobTimes(record, existing, ok, now)
	row, err := s.mapper.RepoBlobRecordToRow(record)
	if err != nil {
		return nil, err
	}
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

func (s *SQLStore) writeRepoBlobRow(ctx context.Context, record *RepoBlobRecord, row repoBlobRow) error {
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

func (s *SQLStore) DeleteRepoBlob(ctx context.Context, key RepoBlobKey) error {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.repoBlobs, sqlRepoBlobRows.Key).Delete(ctx, key.String())
	if err != nil {
		return wrapError(err, "delete repository blob metadata")
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, metadataNow()); err != nil {
		return err
	}
	return nil
}

func (s *SQLStore) ListRepoBlobs(ctx context.Context, opts ...RepoBlobListOption) ([]RepoBlobRecord, error) {
	options := repoBlobListOptions(opts...)
	query := repository.Query(s.repoBlobs)
	if options.RecentFirst {
		query = query.OrderBy(
			sqlRepoBlobRows.LastAccessAt.Desc(),
			sqlRepoBlobRows.LastVerifiedAt.Desc(),
			sqlRepoBlobRows.UpdatedAt.Desc(),
			sqlRepoBlobRows.CreatedAt.Desc(),
			sqlRepoBlobRows.ID.Desc(),
		)
	}
	if options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	rows, err := query.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list repository blob metadata")
	}
	return s.repoBlobRowsToRecords(rows)
}

func (s *SQLStore) repoBlobRowsToRecords(rows interface {
	Values() []repoBlobRow
}) ([]RepoBlobRecord, error) {
	return mapRows(rows, s.mapper.RepoBlobRowToRecord)
}

func (s *SQLStore) updateRepoBlobRow(ctx context.Context, row repoBlobRow) error {
	_, err := repository.By(s.repoBlobs, sqlRepoBlobRows.Key).Update(ctx, row.Key,
		sqlRepoBlobRows.Alias.Set(row.Alias),
		sqlRepoBlobRows.Repository.Set(row.Repository),
		sqlRepoBlobRows.Digest.Set(row.Digest),
		sqlRepoBlobRows.SourceManifest.Set(row.SourceManifest),
		sqlRepoBlobRows.CreatedAt.Set(row.CreatedAt),
		sqlRepoBlobRows.UpdatedAt.Set(row.UpdatedAt),
		sqlRepoBlobRows.LastAccessAt.Set(row.LastAccessAt),
		sqlRepoBlobRows.LastVerifiedAt.Set(row.LastVerifiedAt),
	)
	if err != nil {
		return wrapError(err, "upsert repository blob metadata")
	}
	return nil
}

package meta

import (
	"context"
	"errors"
	"strings"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLStore) Tag(ctx context.Context, key TagKey) (*TagRecord, bool, error) {
	key, err := normalizeTagKey(key)
	if err != nil {
		return nil, false, err
	}
	return s.tagByKey(ctx, key.String())
}

func (s *SQLStore) UpsertTag(ctx context.Context, record TagRecord) (*TagRecord, error) {
	key, record, err := normalizeTagRecord(record)
	if err != nil {
		return nil, err
	}
	now := metadataNow()
	existing, ok, err := s.Tag(ctx, key)
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
	row, err := s.mapper.TagRecordToRow(record)
	if err != nil {
		return nil, err
	}
	if err := s.writeTagRow(ctx, &record, row); err != nil {
		return nil, err
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, record.UpdatedAt); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLStore) writeTagRow(ctx context.Context, record *TagRecord, row tagRow) error {
	if record.ID != 0 {
		if err := s.updateTagRow(ctx, row); err != nil {
			return err
		}
		return nil
	}
	if err := s.tags.Create(ctx, &row); err != nil {
		return wrapError(err, "upsert tag metadata")
	}
	record.ID = row.ID
	return nil
}

func (s *SQLStore) DeleteTag(ctx context.Context, key TagKey) error {
	key, err := normalizeTagKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.tags, sqlTagRows.Key).Delete(ctx, key.String())
	if err != nil {
		return wrapError(err, "delete tag metadata")
	}
	if err := s.refreshRepositoryMetadata(ctx, key.Alias, key.Repository, metadataNow()); err != nil {
		return err
	}
	return nil
}

func (s *SQLStore) GetTag(ctx context.Context, key string) (*TagRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, errorf("%w: tag key is required", ErrInvalidKey)
	}
	return s.tagByKey(ctx, key)
}

func (s *SQLStore) PutTag(ctx context.Context, record TagRecord) error {
	if _, err := s.UpsertTag(ctx, record); err != nil {
		return wrapError(err, "put tag metadata")
	}
	return nil
}

func (s *SQLStore) ListTags(ctx context.Context) ([]TagRecord, error) {
	rows, err := s.tags.List(ctx, nil)
	if err != nil {
		return nil, wrapError(err, "list tag metadata")
	}
	return mapRows(rows, s.mapper.TagRowToRecord)
}

func (s *SQLStore) tagByKey(ctx context.Context, key string) (*TagRecord, bool, error) {
	row, err := repository.By(s.tags, sqlTagRows.Key).Get(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get tag metadata")
	}
	record, err := s.mapper.TagRowToRecord(row)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (s *SQLStore) updateTagRow(ctx context.Context, row tagRow) error {
	return patchRowByKey(ctx, s.tags, sqlTagRows.Key, row.Key, "upsert tag metadata",
		sqlTagRows.Alias.Set(row.Alias),
		sqlTagRows.Repository.Set(row.Repository),
		sqlTagRows.Reference.Set(row.Reference),
		sqlTagRows.Digest.Set(row.Digest),
		sqlTagRows.ExpiresAt.Set(row.ExpiresAt),
		sqlTagRows.CreatedAt.Set(row.CreatedAt),
		sqlTagRows.UpdatedAt.Set(row.UpdatedAt),
	)
}

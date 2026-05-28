package meta

import (
	"context"
	"errors"
	"strings"

	"github.com/arcgolabs/dbx/repository"
)

func (s *SQLiteStore) Tag(ctx context.Context, key TagKey) (*TagRecord, bool, error) {
	key, err := normalizeTagKey(key)
	if err != nil {
		return nil, false, err
	}
	return s.tagByKey(ctx, key.String())
}

func (s *SQLiteStore) UpsertTag(ctx context.Context, record TagRecord) (*TagRecord, error) {
	key, record, err := normalizeTagRecord(record)
	if err != nil {
		return nil, err
	}
	now := sqliteNow()
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
	row := tagRecordToRow(record)
	if err := s.writeTagRow(ctx, &record, row); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLiteStore) writeTagRow(ctx context.Context, record *TagRecord, row tagRow) error {
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

func (s *SQLiteStore) DeleteTag(ctx context.Context, key TagKey) error {
	key, err := normalizeTagKey(key)
	if err != nil {
		return err
	}
	_, err = repository.By(s.tags, sqliteTagRows.Key).Delete(ctx, key.String())
	if err != nil {
		return wrapError(err, "delete tag metadata")
	}
	return nil
}

func (s *SQLiteStore) GetTag(ctx context.Context, key string) (*TagRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, errorf("%w: tag key is required", ErrInvalidKey)
	}
	return s.tagByKey(ctx, key)
}

func (s *SQLiteStore) PutTag(ctx context.Context, record TagRecord) error {
	if _, err := s.UpsertTag(ctx, record); err != nil {
		return wrapError(err, "put tag metadata")
	}
	return nil
}

func (s *SQLiteStore) ListTags(ctx context.Context) ([]TagRecord, error) {
	rows, err := s.tags.List(ctx, nil)
	if err != nil {
		return nil, wrapError(err, "list tag metadata")
	}
	records := make([]TagRecord, 0, rows.Len())
	rows.Range(func(_ int, row tagRow) bool {
		records = append(records, *tagRowToRecord(row))
		return true
	})
	return records, nil
}

func (s *SQLiteStore) tagByKey(ctx context.Context, key string) (*TagRecord, bool, error) {
	row, err := repository.By(s.tags, sqliteTagRows.Key).Get(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrapError(err, "get tag metadata")
	}
	return tagRowToRecord(row), true, nil
}

func (s *SQLiteStore) updateTagRow(ctx context.Context, row tagRow) error {
	_, err := repository.By(s.tags, sqliteTagRows.Key).Update(ctx, row.Key,
		sqliteTagRows.Alias.Set(row.Alias),
		sqliteTagRows.Repository.Set(row.Repository),
		sqliteTagRows.Reference.Set(row.Reference),
		sqliteTagRows.Digest.Set(row.Digest),
		sqliteTagRows.ExpiresAt.Set(row.ExpiresAt),
		sqliteTagRows.CreatedAt.Set(row.CreatedAt),
		sqliteTagRows.UpdatedAt.Set(row.UpdatedAt),
	)
	if err != nil {
		return wrapError(err, "upsert tag metadata")
	}
	return nil
}

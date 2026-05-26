package meta

import (
	"context"
	"strings"
	"time"
)

func (s *BboltStore) Tag(ctx context.Context, key TagKey) (*TagRecord, bool, error) {
	key, err := normalizeTagKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.tags.Get(ctx, key)
	if err != nil {
		return nil, false, wrapError(err, "get tag metadata")
	}
	if !ok {
		return nil, false, nil
	}
	return &record, true, nil
}

func (s *BboltStore) UpsertTag(ctx context.Context, record TagRecord) (*TagRecord, error) {
	key, record, err := normalizeTagRecord(record)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	existing, ok, err := s.Tag(ctx, key)
	if err != nil {
		return nil, err
	}
	if ok {
		record.CreatedAt = existing.CreatedAt
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := s.tags.Put(ctx, key, record); err != nil {
		return nil, wrapError(err, "put tag metadata")
	}
	return &record, nil
}

func (s *BboltStore) DeleteTag(ctx context.Context, key TagKey) error {
	key, err := normalizeTagKey(key)
	if err != nil {
		return err
	}
	if err := s.tags.Delete(ctx, key); err != nil {
		return wrapError(err, "delete tag metadata")
	}
	return nil
}

func (s *BboltStore) GetTag(ctx context.Context, key string) (*TagRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, errorf("%w: tag key is required", ErrInvalidKey)
	}
	entries, err := s.tags.List(ctx)
	if err != nil {
		return nil, false, wrapError(err, "list tag metadata")
	}
	for _, entry := range entries {
		if entry.Value.Key == key {
			record := entry.Value
			return &record, true, nil
		}
	}
	return nil, false, nil
}

func (s *BboltStore) PutTag(ctx context.Context, record TagRecord) error {
	if _, err := s.UpsertTag(ctx, record); err != nil {
		return wrapError(err, "upsert tag metadata")
	}
	return nil
}

func (s *BboltStore) ListTags(ctx context.Context) ([]TagRecord, error) {
	entries, err := s.tags.List(ctx)
	if err != nil {
		return nil, wrapError(err, "list tag metadata")
	}
	records := make([]TagRecord, 0, len(entries))
	for _, entry := range entries {
		records = append(records, entry.Value)
	}
	return records, nil
}

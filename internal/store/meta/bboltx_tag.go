package meta

import (
	"context"
	"fmt"
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
		return nil, false, fmt.Errorf("get tag metadata: %w", err)
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
		return nil, fmt.Errorf("put tag metadata: %w", err)
	}
	return &record, nil
}

func (s *BboltStore) DeleteTag(ctx context.Context, key TagKey) error {
	key, err := normalizeTagKey(key)
	if err != nil {
		return err
	}
	if err := s.tags.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete tag metadata: %w", err)
	}
	return nil
}

func (s *BboltStore) GetTag(ctx context.Context, key string) (*TagRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, fmt.Errorf("%w: tag key is required", ErrInvalidKey)
	}
	entries, err := s.tags.List(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("list tag metadata: %w", err)
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
		return fmt.Errorf("upsert tag metadata: %w", err)
	}
	return nil
}

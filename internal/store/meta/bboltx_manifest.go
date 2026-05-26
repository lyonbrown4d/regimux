package meta

import (
	"context"
	"strings"
	"time"
)

func (s *BboltStore) Manifest(ctx context.Context, key ManifestKey) (*ManifestRecord, bool, error) {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.manifest.Get(ctx, key)
	if err != nil {
		return nil, false, wrapError(err, "get manifest metadata")
	}
	if !ok {
		return nil, false, nil
	}
	record.Headers = cloneHeaders(record.Headers)
	return &record, true, nil
}

func (s *BboltStore) UpsertManifest(ctx context.Context, record ManifestRecord) (*ManifestRecord, error) {
	key, record, err := normalizeManifestRecord(record)
	if err != nil {
		return nil, err
	}
	record = preserveTimes(record, func() (*ManifestRecord, bool, error) {
		return s.Manifest(ctx, key)
	})
	if err := s.manifest.Put(ctx, key, record); err != nil {
		return nil, wrapError(err, "put manifest metadata")
	}
	return &record, nil
}

func (s *BboltStore) DeleteManifest(ctx context.Context, key ManifestKey) error {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return err
	}
	if err := s.manifest.Delete(ctx, key); err != nil {
		return wrapError(err, "delete manifest metadata")
	}
	return nil
}

func (s *BboltStore) GetManifest(ctx context.Context, key string) (*ManifestRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, errorf("%w: manifest key is required", ErrInvalidKey)
	}
	entries, err := s.manifest.List(ctx)
	if err != nil {
		return nil, false, wrapError(err, "list manifest metadata")
	}
	for _, entry := range entries {
		if entry.Value.Key == key {
			record := entry.Value
			record.Headers = cloneHeaders(record.Headers)
			return &record, true, nil
		}
	}
	return nil, false, nil
}

func (s *BboltStore) PutManifest(ctx context.Context, record ManifestRecord) error {
	if _, err := s.UpsertManifest(ctx, record); err != nil {
		return wrapError(err, "upsert manifest metadata")
	}
	return nil
}

func preserveTimes(record ManifestRecord, existing func() (*ManifestRecord, bool, error)) ManifestRecord {
	now := time.Now().UTC()
	if existing != nil {
		current, ok, err := existing()
		if err == nil && ok {
			record.CreatedAt = current.CreatedAt
		}
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	return record
}

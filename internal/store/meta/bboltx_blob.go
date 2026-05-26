package meta

import (
	"context"
	"time"
)

func (s *BboltStore) Blob(ctx context.Context, key BlobKey) (*BlobRecord, bool, error) {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.blobs.Get(ctx, key)
	if err != nil {
		return nil, false, wrapError(err, "get blob metadata")
	}
	if !ok {
		return nil, false, nil
	}
	return &record, true, nil
}

func (s *BboltStore) UpsertBlob(ctx context.Context, record BlobRecord) (*BlobRecord, error) {
	key, record, err := normalizeBlobRecord(record)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	existing, ok, err := s.Blob(ctx, key)
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
	if err := s.blobs.Put(ctx, key, record); err != nil {
		return nil, wrapError(err, "put blob metadata")
	}
	return &record, nil
}

func (s *BboltStore) DeleteBlob(ctx context.Context, key BlobKey) error {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return err
	}
	if err := s.blobs.Delete(ctx, key); err != nil {
		return wrapError(err, "delete blob metadata")
	}
	return nil
}

func (s *BboltStore) GetBlob(ctx context.Context, digest string) (*BlobRecord, bool, error) {
	return s.Blob(ctx, BlobKey{Digest: digest})
}

func (s *BboltStore) PutBlob(ctx context.Context, record BlobRecord) error {
	if _, err := s.UpsertBlob(ctx, record); err != nil {
		return wrapError(err, "upsert blob metadata")
	}
	return nil
}

func (s *BboltStore) RepoBlob(ctx context.Context, key RepoBlobKey) (*RepoBlobRecord, bool, error) {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.repoBlob.Get(ctx, key)
	if err != nil {
		return nil, false, wrapError(err, "get repository blob metadata")
	}
	if !ok {
		return nil, false, nil
	}
	return &record, true, nil
}

func (s *BboltStore) UpsertRepoBlob(ctx context.Context, record RepoBlobRecord) (*RepoBlobRecord, error) {
	key, record, err := normalizeRepoBlobRecord(record)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	existing, ok, err := s.RepoBlob(ctx, key)
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
	if record.LastVerifiedAt.IsZero() {
		record.LastVerifiedAt = now
	}
	if err := s.repoBlob.Put(ctx, key, record); err != nil {
		return nil, wrapError(err, "put repository blob metadata")
	}
	return &record, nil
}

func (s *BboltStore) DeleteRepoBlob(ctx context.Context, key RepoBlobKey) error {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return err
	}
	if err := s.repoBlob.Delete(ctx, key); err != nil {
		return wrapError(err, "delete repository blob metadata")
	}
	return nil
}

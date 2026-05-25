package meta

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcgolabs/storx/bboltx"
	"github.com/arcgolabs/storx/codec"
	"github.com/arcgolabs/storx/keycodec"
	"go.etcd.io/bbolt"
)

const (
	bucketManifests = "manifests"
	bucketTags      = "tags"
	bucketBlobs     = "blobs"
	bucketRepoBlobs = "repo_blobs"
)

type BboltOptions struct {
	Path   string
	Logger *slog.Logger
}

type BboltStore struct {
	db       *bboltx.DB
	manifest *bboltx.Bucket[ManifestKey, ManifestRecord]
	tags     *bboltx.Bucket[TagKey, TagRecord]
	blobs    *bboltx.Bucket[BlobKey, BlobRecord]
	repoBlob *bboltx.Bucket[RepoBlobKey, RepoBlobRecord]
}

func OpenBbolt(path string, logger *slog.Logger) (*BboltStore, error) {
	return OpenBboltWithOptions(context.Background(), BboltOptions{
		Path:   path,
		Logger: logger,
	})
}

func OpenBboltWithOptions(ctx context.Context, opts BboltOptions) (*BboltStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil, fmt.Errorf("%w: bbolt path is required", ErrInvalidValue)
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create bbolt directory: %w", err)
	}

	db, err := bboltx.Open(path, 0o600, &bbolt.Options{Timeout: time.Second}, bboltx.WithDBLogger(opts.Logger))
	if err != nil {
		return nil, err
	}

	store := &BboltStore{
		db:       db,
		manifest: bboltx.NewBucketWithDB(db, bucketManifests, manifestKeyCodec(), codec.JSON[ManifestRecord]()),
		tags:     bboltx.NewBucketWithDB(db, bucketTags, tagKeyCodec(), codec.JSON[TagRecord]()),
		blobs:    bboltx.NewBucketWithDB(db, bucketBlobs, blobKeyCodec(), codec.JSON[BlobRecord]()),
		repoBlob: bboltx.NewBucketWithDB(db, bucketRepoBlobs, repoBlobKeyCodec(), codec.JSON[RepoBlobRecord]()),
	}
	if err := store.bootstrap(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *BboltStore) bootstrap(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Raw().Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{[]byte(bucketManifests), []byte(bucketTags), []byte(bucketBlobs), []byte(bucketRepoBlobs)} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *BboltStore) UpstreamByAlias(context.Context, string) (*Upstream, error) {
	return nil, ErrNotFound
}

func (s *BboltStore) RepositoryByName(context.Context, int64, string) (*Repository, error) {
	return nil, ErrNotFound
}

func (s *BboltStore) Manifest(ctx context.Context, key ManifestKey) (*ManifestRecord, bool, error) {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.manifest.Get(ctx, key)
	if err != nil || !ok {
		return nil, ok, err
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
		return nil, err
	}
	return &record, nil
}

func (s *BboltStore) DeleteManifest(ctx context.Context, key ManifestKey) error {
	key, err := normalizeManifestKey(key)
	if err != nil {
		return err
	}
	return s.manifest.Delete(ctx, key)
}

func (s *BboltStore) GetManifest(ctx context.Context, key string) (*ManifestRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, fmt.Errorf("%w: manifest key is required", ErrInvalidKey)
	}
	entries, err := s.manifest.List(ctx)
	if err != nil {
		return nil, false, err
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
	_, err := s.UpsertManifest(ctx, record)
	return err
}

func (s *BboltStore) Tag(ctx context.Context, key TagKey) (*TagRecord, bool, error) {
	key, err := normalizeTagKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.tags.Get(ctx, key)
	if err != nil || !ok {
		return nil, ok, err
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
		return nil, err
	}
	return &record, nil
}

func (s *BboltStore) DeleteTag(ctx context.Context, key TagKey) error {
	key, err := normalizeTagKey(key)
	if err != nil {
		return err
	}
	return s.tags.Delete(ctx, key)
}

func (s *BboltStore) GetTag(ctx context.Context, key string) (*TagRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, fmt.Errorf("%w: tag key is required", ErrInvalidKey)
	}
	entries, err := s.tags.List(ctx)
	if err != nil {
		return nil, false, err
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
	_, err := s.UpsertTag(ctx, record)
	return err
}

func (s *BboltStore) Blob(ctx context.Context, key BlobKey) (*BlobRecord, bool, error) {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.blobs.Get(ctx, key)
	if err != nil || !ok {
		return nil, ok, err
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
		return nil, err
	}
	return &record, nil
}

func (s *BboltStore) DeleteBlob(ctx context.Context, key BlobKey) error {
	key, err := normalizeBlobKey(key)
	if err != nil {
		return err
	}
	return s.blobs.Delete(ctx, key)
}

func (s *BboltStore) GetBlob(ctx context.Context, digest string) (*BlobRecord, bool, error) {
	return s.Blob(ctx, BlobKey{Digest: digest})
}

func (s *BboltStore) PutBlob(ctx context.Context, record BlobRecord) error {
	_, err := s.UpsertBlob(ctx, record)
	return err
}

func (s *BboltStore) RepoBlob(ctx context.Context, key RepoBlobKey) (*RepoBlobRecord, bool, error) {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.repoBlob.Get(ctx, key)
	if err != nil || !ok {
		return nil, ok, err
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
		return nil, err
	}
	return &record, nil
}

func (s *BboltStore) DeleteRepoBlob(ctx context.Context, key RepoBlobKey) error {
	key, err := normalizeRepoBlobKey(key)
	if err != nil {
		return err
	}
	return s.repoBlob.Delete(ctx, key)
}

func (s *BboltStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func manifestKeyCodec() keycodec.Codec[ManifestKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key ManifestKey) string { return key.Alias },
			func(target *ManifestKey, value string) { target.Alias = value },
		),
		keycodec.Field(keycodec.String(),
			func(key ManifestKey) string { return key.Repository },
			func(target *ManifestKey, value string) { target.Repository = value },
		),
		keycodec.Field(keycodec.String(),
			func(key ManifestKey) string { return key.Digest },
			func(target *ManifestKey, value string) { target.Digest = value },
		),
	)
}

func tagKeyCodec() keycodec.Codec[TagKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key TagKey) string { return key.Alias },
			func(target *TagKey, value string) { target.Alias = value },
		),
		keycodec.Field(keycodec.String(),
			func(key TagKey) string { return key.Repository },
			func(target *TagKey, value string) { target.Repository = value },
		),
		keycodec.Field(keycodec.String(),
			func(key TagKey) string { return key.Reference },
			func(target *TagKey, value string) { target.Reference = value },
		),
	)
}

func blobKeyCodec() keycodec.Codec[BlobKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key BlobKey) string { return key.Digest },
			func(target *BlobKey, value string) { target.Digest = value },
		),
	)
}

func repoBlobKeyCodec() keycodec.Codec[RepoBlobKey] {
	return keycodec.Composite(
		keycodec.Field(keycodec.String(),
			func(key RepoBlobKey) string { return key.Alias },
			func(target *RepoBlobKey, value string) { target.Alias = value },
		),
		keycodec.Field(keycodec.String(),
			func(key RepoBlobKey) string { return key.Repository },
			func(target *RepoBlobKey, value string) { target.Repository = value },
		),
		keycodec.Field(keycodec.String(),
			func(key RepoBlobKey) string { return key.Digest },
			func(target *RepoBlobKey, value string) { target.Digest = value },
		),
	)
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

var _ Store = (*BboltStore)(nil)

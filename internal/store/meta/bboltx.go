// Package meta stores registry metadata records.
package meta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arcgolabs/storx/bboltx"
	"github.com/arcgolabs/storx/codec"
	"go.etcd.io/bbolt"
)

const (
	bucketManifests = "manifests"
	bucketTags      = "tags"
	bucketBlobs     = "blobs"
	bucketRepoBlobs = "repo_blobs"
)

var metadataBuckets = []string{
	bucketManifests,
	bucketTags,
	bucketBlobs,
	bucketRepoBlobs,
}

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
	if err := requireMetadataContext(ctx, "open bbolt metadata store"); err != nil {
		return nil, err
	}

	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil, fmt.Errorf("%w: bbolt path is required", ErrInvalidValue)
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create bbolt directory: %w", err)
	}

	db, err := bboltx.Open(path, 0o600, &bbolt.Options{Timeout: time.Second}, bboltx.WithDBLogger(opts.Logger))
	if err != nil {
		return nil, fmt.Errorf("open bbolt metadata db: %w", err)
	}

	store := &BboltStore{
		db:       db,
		manifest: bboltx.NewBucketWithDB(db, bucketManifests, manifestKeyCodec(), codec.JSON[ManifestRecord]()),
		tags:     bboltx.NewBucketWithDB(db, bucketTags, tagKeyCodec(), codec.JSON[TagRecord]()),
		blobs:    bboltx.NewBucketWithDB(db, bucketBlobs, blobKeyCodec(), codec.JSON[BlobRecord]()),
		repoBlob: bboltx.NewBucketWithDB(db, bucketRepoBlobs, repoBlobKeyCodec(), codec.JSON[RepoBlobRecord]()),
	}
	if err := store.bootstrap(ctx); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close bbolt metadata db: %w", closeErr))
		}
		return nil, err
	}
	return store, nil
}

func (s *BboltStore) bootstrap(ctx context.Context) error {
	if err := requireMetadataContext(ctx, "bootstrap bbolt metadata store"); err != nil {
		return err
	}
	if err := s.db.Raw().Update(func(tx *bbolt.Tx) error {
		for _, name := range metadataBuckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("create metadata bucket %s: %w", name, err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("bootstrap bbolt metadata buckets: %w", err)
	}
	return nil
}

func requireMetadataContext(ctx context.Context, operation string) error {
	if ctx == nil {
		return fmt.Errorf("%w: %s context is required", ErrInvalidValue, operation)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s context: %w", operation, err)
	}
	return nil
}

func (s *BboltStore) UpstreamByAlias(context.Context, string) (*Upstream, error) {
	return nil, ErrNotFound
}

func (s *BboltStore) RepositoryByName(context.Context, int64, string) (*Repository, error) {
	return nil, ErrNotFound
}

func (s *BboltStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close bbolt metadata db: %w", err)
	}
	return nil
}

var _ Store = (*BboltStore)(nil)

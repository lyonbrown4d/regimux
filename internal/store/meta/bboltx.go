// Package meta stores registry metadata records.
package meta

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/storx/bboltx"
	"github.com/arcgolabs/storx/codec"
	"go.etcd.io/bbolt"
)

const (
	bucketManifests = "manifests"
	bucketTags      = "tags"
	bucketPulls     = "pulls"
	bucketBlobs     = "blobs"
	bucketRepoBlobs = "repo_blobs"
)

var metadataBuckets = collectionlist.NewList(
	bucketManifests,
	bucketTags,
	bucketPulls,
	bucketBlobs,
	bucketRepoBlobs,
)

type BboltOptions struct {
	Path   string
	Logger *slog.Logger
}

type BboltStore struct {
	db       *bboltx.DB
	manifest *bboltx.Bucket[ManifestKey, ManifestRecord]
	tags     *bboltx.Bucket[TagKey, TagRecord]
	pulls    *bboltx.Bucket[PullKey, PullRecord]
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
		return nil, errorf("%w: bbolt path is required", ErrInvalidValue)
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, wrapError(err, "create bbolt directory")
	}

	db, err := bboltx.Open(path, 0o600, &bbolt.Options{Timeout: time.Second}, bboltx.WithDBLogger(opts.Logger))
	if err != nil {
		return nil, wrapError(err, "open bbolt metadata db")
	}

	store := &BboltStore{
		db:       db,
		manifest: bboltx.NewBucketWithDB(db, bucketManifests, manifestKeyCodec(), codec.JSON[ManifestRecord]()),
		tags:     bboltx.NewBucketWithDB(db, bucketTags, tagKeyCodec(), codec.JSON[TagRecord]()),
		pulls:    bboltx.NewBucketWithDB(db, bucketPulls, pullKeyCodec(), codec.JSON[PullRecord]()),
		blobs:    bboltx.NewBucketWithDB(db, bucketBlobs, blobKeyCodec(), codec.JSON[BlobRecord]()),
		repoBlob: bboltx.NewBucketWithDB(db, bucketRepoBlobs, repoBlobKeyCodec(), codec.JSON[RepoBlobRecord]()),
	}
	if err := store.bootstrap(ctx); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, errors.Join(err, wrapError(closeErr, "close bbolt metadata db"))
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
		for _, name := range metadataBuckets.Values() {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return wrapError(err, "create metadata bucket %s", name)
			}
		}
		return nil
	}); err != nil {
		return wrapError(err, "bootstrap bbolt metadata buckets")
	}
	return nil
}

func requireMetadataContext(ctx context.Context, operation string) error {
	if ctx == nil {
		return errorf("%w: %s context is required", ErrInvalidValue, operation)
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "%s context", operation)
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
		return wrapError(err, "close bbolt metadata db")
	}
	return nil
}

var _ Store = (*BboltStore)(nil)

package artifactcache

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/samber/oops"
)

func New(deps Dependencies) *Store {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Store{
		metadata: deps.Metadata,
		objects:  deps.Objects,
		logger:   logger.With("component", "artifact-cache"),
		now:      now,
	}
}

func (s *Store) Get(ctx context.Context, key Key) (Entry, bool, error) {
	if s == nil || s.metadata == nil || s.objects == nil {
		return Entry{}, false, nil
	}
	tag, manifest, ok, err := s.lookup(ctx, key)
	if err != nil || !ok {
		return Entry{}, false, err
	}
	entry, ok, err := s.open(ctx, *manifest)
	if err != nil || !ok {
		return Entry{}, ok, err
	}
	now := s.now()
	entry.Expired = expiredAt(tag.ExpiresAt, now) || manifest.Expired(now)
	return entry, true, nil
}

func (s *Store) lookup(ctx context.Context, key Key) (*meta.TagRecord, *meta.ManifestRecord, bool, error) {
	tag, ok, err := s.metadata.Tag(ctx, meta.TagKey{
		Alias:      key.Alias,
		Repository: key.Repository,
		Reference:  key.Reference,
	})
	if err != nil {
		return nil, nil, false, wrapError(err, "lookup artifact cache tag")
	}
	if !ok {
		return nil, nil, false, nil
	}
	manifest, ok, err := s.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      key.Alias,
		Repository: key.Repository,
		Digest:     tag.Digest,
	})
	if err != nil {
		return nil, nil, false, wrapError(err, "lookup artifact cache manifest")
	}
	if !ok {
		return nil, nil, false, nil
	}
	return tag, manifest, true, nil
}

func (s *Store) open(ctx context.Context, manifest meta.ManifestRecord) (Entry, bool, error) {
	objectKey := manifest.ObjectKey
	if objectKey == "" {
		objectKey = manifest.Digest
	}
	reader, info, err := s.objects.Get(ctx, objectKey, object.GetOptions{})
	if errors.Is(err, object.ErrNotFound) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, wrapError(err, "open cached artifact object")
	}
	size := manifest.Size
	if size <= 0 && info != nil {
		size = info.Size
	}
	return Entry{
		Digest:  manifest.Digest,
		Size:    size,
		Headers: http.Header(manifest.Headers).Clone(),
		Body:    reader,
	}, true, nil
}

func expiredAt(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !now.Before(expiresAt)
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("artifact-cache").Wrapf(err, "%s", message)
}

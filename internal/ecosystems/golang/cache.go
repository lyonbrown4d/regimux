package golang

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type cachedMetadata struct {
	tag      *meta.TagRecord
	manifest *meta.ManifestRecord
}

func (s *Service) cached(ctx context.Context, requestRoute route) (storedResponse, bool, error) {
	metadata, ok, err := s.cachedMetadata(ctx, requestRoute)
	if err != nil || !ok {
		return storedResponse{}, false, err
	}
	return s.openCachedObject(ctx, metadata)
}

func (s *Service) cachedMetadata(ctx context.Context, requestRoute route) (cachedMetadata, bool, error) {
	if s.metadata == nil || s.objects == nil {
		return cachedMetadata{}, false, nil
	}
	tag, ok, err := s.metadata.Tag(ctx, meta.TagKey{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Module,
		Reference:  requestRoute.Reference,
	})
	if err != nil {
		return cachedMetadata{}, false, wrapError(err, "lookup go proxy cache metadata")
	}
	if !ok {
		return cachedMetadata{}, false, nil
	}
	manifest, ok, err := s.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Module,
		Digest:     tag.Digest,
	})
	if err != nil {
		return cachedMetadata{}, false, wrapError(err, "lookup go proxy content metadata")
	}
	if !ok {
		return cachedMetadata{}, false, nil
	}
	return cachedMetadata{tag: tag, manifest: manifest}, true, nil
}

func (s *Service) openCachedObject(ctx context.Context, metadata cachedMetadata) (storedResponse, bool, error) {
	objectKey := metadata.manifest.ObjectKey
	if objectKey == "" {
		objectKey = metadata.manifest.Digest
	}
	reader, info, err := s.objects.Get(ctx, objectKey, object.GetOptions{})
	if errors.Is(err, object.ErrNotFound) {
		return storedResponse{}, false, nil
	}
	if err != nil {
		return storedResponse{}, false, wrapError(err, "open cached go proxy object")
	}
	size := metadata.manifest.Size
	if size <= 0 && info != nil {
		size = info.Size
	}
	now := time.Now().UTC()
	return storedResponse{
		digest:  metadata.manifest.Digest,
		size:    size,
		headers: http.Header(metadata.manifest.Headers).Clone(),
		body:    reader,
		expired: expiredAt(metadata.tag.ExpiresAt, now) || metadata.manifest.Expired(now),
	}, true, nil
}

func artifactKey(requestRoute route) artifactcache.Key {
	return artifactcache.Key{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Module,
		Reference:  requestRoute.Reference,
	}
}

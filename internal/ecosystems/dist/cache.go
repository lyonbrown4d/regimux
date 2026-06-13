package dist

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (s *Service) cached(ctx context.Context, req Request, requestRoute Route) (storedResponse, bool, error) {
	if s == nil || s.metadata == nil || s.objects == nil {
		return storedResponse{}, false, nil
	}
	tag, manifest, ok, err := s.cachedMetadata(ctx, requestRoute)
	if err != nil || !ok {
		return storedResponse{}, false, err
	}
	return s.openCachedObject(ctx, req, *tag, *manifest)
}

func (s *Service) cachedMetadata(ctx context.Context, requestRoute Route) (*meta.TagRecord, *meta.ManifestRecord, bool, error) {
	tag, ok, err := s.metadata.Tag(ctx, meta.TagKey{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
	})
	if err != nil {
		return nil, nil, false, wrapError(err, "lookup dist cache tag")
	}
	if !ok {
		return nil, nil, false, nil
	}
	manifest, ok, err := s.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Digest:     tag.Digest,
	})
	if err != nil {
		return nil, nil, false, wrapError(err, "lookup dist cache manifest")
	}
	if !ok {
		return nil, nil, false, nil
	}
	return tag, manifest, true, nil
}

func (s *Service) openCachedObject(ctx context.Context, req Request, tag meta.TagRecord, manifest meta.ManifestRecord) (storedResponse, bool, error) {
	headers := http.Header(manifest.Headers).Clone()
	opts, status, responseSize, err := cachedGetOptions(req, manifest.Size, headers)
	if err != nil {
		return storedResponse{}, false, err
	}
	reader, _, err := s.objects.Get(ctx, objectKey(manifest), opts)
	if errors.Is(err, object.ErrNotFound) {
		return storedResponse{}, false, nil
	}
	if err != nil {
		return storedResponse{}, false, wrapError(err, "open cached dist object")
	}
	return storedResponse{
		digest:  manifest.Digest,
		size:    responseSize,
		headers: headers,
		body:    reader,
		expired: artifactExpired(tag.ExpiresAt, s.now()) || manifest.Expired(s.now()),
		status:  status,
	}, true, nil
}

func objectKey(manifest meta.ManifestRecord) string {
	if manifest.ObjectKey != "" {
		return manifest.ObjectKey
	}
	return manifest.Digest
}

func cachedGetOptions(req Request, size int64, headers http.Header) (object.GetOptions, int, int64, error) {
	if methodOrGet(req.Method) != http.MethodGet || strings.TrimSpace(req.Range) == "" {
		return object.GetOptions{}, http.StatusOK, size, nil
	}
	resolved, err := resolveCachedRange(req.Range, size)
	if err != nil {
		return object.GetOptions{}, 0, 0, err
	}
	if resolved == nil {
		return object.GetOptions{}, http.StatusOK, size, nil
	}
	headers.Set(distribution.HeaderContentRange, resolved.ContentRange(size))
	headers.Set(distribution.HeaderContentLength, formatInt64(resolved.Length()))
	return object.GetOptions{Range: resolved}, http.StatusPartialContent, resolved.Length(), nil
}

func resolveCachedRange(header string, size int64) (*object.HTTPRange, error) {
	parsed, err := object.ParseRange(header)
	if err != nil {
		return nil, wrapError(errors.Join(errInvalidRange, err), "parse dist range")
	}
	resolved, err := parsed.Resolve(size)
	if err != nil {
		return nil, wrapError(errors.Join(errInvalidRange, err), "resolve dist range")
	}
	return resolved, nil
}

func artifactKey(requestRoute Route) artifactcache.Key {
	return artifactcache.Key{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
	}
}

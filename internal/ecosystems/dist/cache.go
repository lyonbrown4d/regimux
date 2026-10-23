package dist

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
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
	tag, manifest, ok, err := artifactcache.LookupMetadata(ctx, s.cache, artifactKey(requestRoute))
	if err != nil {
		return nil, nil, false, wrapError(err, "lookup dist cache metadata")
	}
	if !ok {
		return nil, nil, false, nil
	}
	return &tag, &manifest, true, nil
}

func (s *Service) openCachedObject(ctx context.Context, req Request, tag meta.TagRecord, manifest meta.ManifestRecord) (storedResponse, bool, error) {
	headers := http.Header(manifest.Headers).Clone()
	opts, status, responseSize := cachedGetOptions(req, manifest.Size, headers)
	reader := io.ReadCloser(http.NoBody)
	var err error
	if status != http.StatusRequestedRangeNotSatisfiable {
		reader, _, err = s.openRecoverableObject(ctx, objectKey(manifest), manifest.Digest, manifest.Size, opts)
	}
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

func cachedGetOptions(req Request, size int64, headers http.Header) (object.GetOptions, int, int64) {
	if methodOrGet(req.Method) != http.MethodGet || strings.TrimSpace(req.Range) == "" || !transfer.MatchesIfRange(req.IfRange, headers) {
		return object.GetOptions{}, http.StatusOK, size
	}
	resolved, err := resolveCachedRange(req.Range, size)
	if err != nil {
		headers.Set(distribution.HeaderContentRange, "bytes */"+formatInt64(size))
		headers.Set(distribution.HeaderContentLength, "0")
		return object.GetOptions{}, http.StatusRequestedRangeNotSatisfiable, 0
	}
	if resolved == nil {
		return object.GetOptions{}, http.StatusOK, size
	}
	headers.Set(distribution.HeaderContentRange, resolved.ContentRange(size))
	headers.Set(distribution.HeaderContentLength, formatInt64(resolved.Length()))
	return object.GetOptions{Range: resolved}, http.StatusPartialContent, resolved.Length()
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

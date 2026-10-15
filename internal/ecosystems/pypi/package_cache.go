package pypi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func (s *Service) cachedPackage(ctx context.Context, req Request, route Route) *Response {
	manifest, ok := s.packageManifest(ctx, route)
	if !ok {
		return nil
	}
	if err := artifactcache.Touch(ctx, s.metadata, *manifest, s.now()); err != nil {
		s.logger.WarnContext(ctx, "touch PyPI cache access failed", "digest", manifest.Digest, "error", err)
	}
	if !packageIsMetadata(route) && validSHA256(req.SHA256) && !strings.EqualFold(manifest.Digest, "sha256:"+req.SHA256) {
		return nil
	}
	resp, opts := cachedPackageResponse(req, route, manifest)
	if methodOrGet(req.Method) == http.MethodHead || resp.Status == http.StatusRequestedRangeNotSatisfiable {
		return resp
	}
	body, err := s.openCachedPackage(ctx, manifest, opts)
	if err != nil {
		s.logger.WarnContext(ctx, "pypi package cache read failed; fetching upstream", "alias", route.Alias)
		s.observePackageCache(ctx, route.Alias, "read_error")
		return nil
	}
	resp.Body = body
	return resp
}

func (s *Service) packageManifest(ctx context.Context, route Route) (*meta.ManifestRecord, bool) {
	if s.cache == nil || s.cache.Metadata() == nil || s.cache.Objects() == nil {
		return nil, false
	}
	metadataCtx, cancel := context.WithTimeout(ctx, pypiCacheTimeout)
	defer cancel()
	tag, ok, err := s.cache.Metadata().Tag(metadataCtx, meta.TagKey{Alias: route.Alias, Repository: route.Repository, Reference: route.Reference})
	if err != nil || !ok {
		if err != nil {
			s.observePackageCache(ctx, route.Alias, "read_error")
		}
		return nil, false
	}
	manifest, ok, err := s.cache.Metadata().Manifest(metadataCtx, meta.ManifestKey{Alias: route.Alias, Repository: route.Repository, Digest: tag.Digest})
	return manifest, ok && err == nil
}

func cachedPackageResponse(req Request, route Route, manifest *meta.ManifestRecord) (*Response, object.GetOptions) {
	headers := cacheHeaders(http.Header(manifest.Headers), manifest.Size)
	headers.Set("Accept-Ranges", "bytes")
	headers.Set(headerMirrorCache, cacheHit)
	resp := &Response{Status: http.StatusOK, Headers: headers, Body: http.NoBody, Size: manifest.Size, Cache: cacheHit, ContentType: routeContentType(route, headers)}
	opts := object.GetOptions{}
	if methodOrGet(req.Method) == http.MethodGet && req.Range != "" && matchesIfRange(req.IfRange, headers) {
		opts.Range = applyCachedRange(req.Range, resp)
	}
	return resp, opts
}

func applyCachedRange(header string, resp *Response) *object.HTTPRange {
	requested, err := object.ParseRange(header)
	var resolved *object.HTTPRange
	if err == nil && requested != nil {
		resolved, err = requested.Resolve(resp.Size)
	}
	if err != nil || resolved == nil {
		resp.Headers.Set("Content-Range", fmt.Sprintf("bytes */%d", resp.Size))
		resp.Headers.Set("Content-Length", "0")
		resp.Status, resp.Size = http.StatusRequestedRangeNotSatisfiable, 0
		return nil
	}
	resp.Headers.Set("Content-Range", resolved.ContentRange(resp.Size))
	resp.Size, resp.Status = resolved.Length(), http.StatusPartialContent
	resp.Headers.Set("Content-Length", strconv.FormatInt(resp.Size, 10))
	return resolved
}

func (s *Service) openCachedPackage(ctx context.Context, manifest *meta.ManifestRecord, opts object.GetOptions) (io.ReadCloser, error) {
	objectCtx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(pypiCacheTimeout, cancel)
	key := manifest.ObjectKey
	if key == "" {
		key = manifest.Digest
	}
	body, _, err := s.cache.Objects().Get(objectCtx, key, opts)
	timer.Stop()
	if err == nil {
		err = objectCtx.Err()
	}
	if err != nil {
		cancel()
		closeReadCloser(body, s.logger, "close failed pypi cache body")
		return nil, wrapError(err, "open cached pypi package")
	}
	return upstreamhttp.WithIdleTimeout(body, cancel, pypiBodyIdleTimeout), nil
}

func matchesIfRange(value string, headers http.Header) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, `W/`) {
		return false
	}
	if strings.HasPrefix(value, `"`) {
		return value == headers.Get("ETag")
	}
	modified, err := http.ParseTime(headers.Get("Last-Modified"))
	when, whenErr := http.ParseTime(value)
	return err == nil && whenErr == nil && modified.Equal(when)
}

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
	"github.com/lyonbrown4d/regimux/internal/transfer"
)

// The second result distinguishes a failed cache read from a normal miss.
func (s *Service) cachedPackage(ctx context.Context, req Request, route Route) (*Response, bool) {
	manifest, ok, err := s.packageManifest(ctx, route)
	if !ok {
		return nil, err != nil
	}
	if !packageIsMetadata(route) && validSHA256(req.SHA256) && !strings.EqualFold(manifest.Digest, "sha256:"+req.SHA256) {
		return nil, false
	}
	resp, opts := cachedPackageResponse(req, route, manifest)
	if methodOrGet(req.Method) == http.MethodHead || resp.Status == http.StatusRequestedRangeNotSatisfiable {
		return resp, false
	}
	body, err := s.openCachedPackage(ctx, manifest, opts)
	if err != nil {
		s.logger.WarnContext(ctx, "pypi package cache read failed; fetching upstream", "alias", route.Alias)
		s.observePackageCache(ctx, route.Alias, "read_error")
		return nil, true
	}
	resp.Body = body
	return resp, false
}

func (s *Service) packageManifest(ctx context.Context, route Route) (*meta.ManifestRecord, bool, error) {
	if s.cache == nil || s.cache.Metadata() == nil || s.cache.Objects() == nil {
		return nil, false, nil
	}
	metadataCtx, cancel := context.WithTimeout(ctx, pypiCacheTimeout)
	defer cancel()
	started := time.Now()
	manifest, ok, err := artifactcache.LookupManifest(metadataCtx, s.cache, artifactKey(route))
	if err != nil || !ok {
		if err != nil {
			s.logger.WarnContext(ctx, "pypi package metadata read failed; fetching upstream",
				"alias", route.Alias, "duration", time.Since(started), "error", err)
			s.observePackageCache(ctx, route.Alias, "read_error")
		}
		return nil, false, wrapError(err, "read pypi package manifest")
	}
	return manifest, true, nil
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
	key := manifest.ObjectKey
	if key == "" {
		key = manifest.Digest
	}
	body, _, err := transfer.OpenObject(ctx, s.cache.Objects(), transfer.ObjectRequest{
		Key: key, Digest: manifest.Digest, Size: manifest.Size, Options: opts,
		OpenTimeout: pypiCacheTimeout, IdleTimeout: pypiBodyIdleTimeout,
	})
	if err != nil {
		return nil, wrapError(err, "open cached pypi package")
	}
	return body, nil
}

func matchesIfRange(value string, headers http.Header) bool {
	return transfer.MatchesIfRange(value, headers)
}

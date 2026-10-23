package pypi

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const (
	pypiCacheTimeout          = 2 * time.Second
	pypiCacheWriteTimeout     = 30 * time.Second
	pypiBodyIdleTimeout       = upstreamhttp.DefaultBodyIdleTimeout
	pypiDefaultAttemptTimeout = 10 * time.Second
	pypiFillWaitTimeout       = 5 * time.Minute
)

func (s *Service) getPackage(ctx context.Context, req Request, route Route, cfg config.UpstreamConfig, mode requestMode) (*Response, error) {
	req, route = packageIdentity(req, route)
	resp, cacheFailed := s.clientCachedPackage(ctx, req, route, mode)
	if resp != nil {
		return resp, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, wrapError(err, "request pypi package")
	}
	cached, finish, err := s.beginPackageFill(ctx, req, route, mode, !cacheFailed)
	if cached != nil || err != nil {
		return cached, err
	}
	fetched, err := s.fetchRangedPackage(ctx, req, route, cfg)
	if err != nil {
		finish()
		return nil, err
	}
	resp = s.responseFromUpstream(req, route, fetched)
	if shouldCachePackage(req, fetched.status) {
		req.SHA256 = fetched.expectedSHA256
		if packageIsMetadata(route) {
			req.SHA256 = ""
		}
		resp.Body = s.packageCacheBody(ctx, req, route, fetched, finish)
	} else {
		finish()
	}
	return resp, nil
}

func (s *Service) clientCachedPackage(ctx context.Context, req Request, route Route, mode requestMode) (*Response, bool) {
	if mode != requestModeClient {
		return nil, false
	}
	return s.cachedPackage(ctx, req, route)
}

func shouldCachePackage(req Request, status int) bool {
	return status == http.StatusOK && methodOrGet(req.Method) == http.MethodGet && req.Range == ""
}

func noPackageFill() {}

func (s *Service) beginPackageFill(ctx context.Context, req Request, route Route, mode requestMode, recheckCache bool) (*Response, func(), error) {
	if !shouldCachePackage(req, http.StatusOK) {
		return nil, noPackageFill, nil
	}
	key := artifactKey(route)
	waitCtx, cancel := context.WithTimeout(ctx, pypiFillWaitTimeout)
	defer cancel()
	// One replacement fill may recover an aborted owner; repeated failures retain the bounded fallback.
	for range 2 {
		fill, owner := s.fills.Begin(key)
		if owner {
			cached, finish := s.packageFillOwner(ctx, req, route, mode, recheckCache, func() { s.fills.Finish(key, fill, nil) })
			return cached, finish, nil
		}
		err := fill.Wait(waitCtx)
		if ctx.Err() != nil {
			return nil, noPackageFill, wrapError(ctx.Err(), "wait for pypi package fill")
		}
		if err != nil {
			return nil, noPackageFill, nil
		}
		cached, cacheFailed := s.clientCachedPackage(ctx, req, route, mode)
		if cached != nil {
			return cached, noPackageFill, nil
		}
		recheckCache = !cacheFailed
	}
	return nil, noPackageFill, nil
}

func (s *Service) packageFillOwner(ctx context.Context, req Request, route Route, mode requestMode, recheckCache bool, finish func()) (*Response, func()) {
	if !recheckCache {
		return nil, finish
	}
	cached, _ := s.clientCachedPackage(ctx, req, route, mode)
	if cached != nil {
		finish()
		return cached, noPackageFill
	}
	return nil, finish
}

func packageURLs(route Route, cfg config.UpstreamConfig) []string {
	if route.DirectURL != "" {
		return []string{urlWithQuery(route.DirectURL, route.Query)}
	}
	urls := make([]string, 0, 1+len(cfg.Mirrors))
	for _, endpoint := range append([]string{cfg.Registry}, cfg.Mirrors...) {
		if endpoint != "" {
			urls = append(urls, urlWithQuery(strings.TrimRight(endpoint, "/")+"/"+route.UpstreamTail, route.Query))
		}
	}
	return urls
}

func (s *Service) closePackageFetch(fetched *upstreamFetch) {
	if fetched != nil {
		closeReadCloser(fetched.body, s.logger, "close unused pypi endpoint response")
	}
}

func (s *Service) fetchPackage(ctx context.Context, req Request, route Route, cfg config.UpstreamConfig) (*upstreamFetch, error) {
	urls := packageURLs(route, cfg)
	var last *upstreamFetch
	var lastErr error
	for i := 0; i < len(urls) && i < 9; i++ {
		s.closePackageFetch(last)
		fetched, err := s.fetchPackageURL(ctx, req, cfg, urls[i])
		if ctx.Err() != nil {
			s.closePackageFetch(fetched)
			return nil, wrapError(ctx.Err(), "fetch pypi package")
		}
		if err == nil && !retryablePackageStatus(fetched.status) {
			fetched.expectedSHA256 = req.SHA256
			return fetched, nil
		}
		last, lastErr = fetched, err
		if i == 0 {
			alternatives, digest := s.packageAlternatives(ctx, req, route, cfg, urls)
			req.SHA256 = digest
			urls = append(urls, alternatives...)
		}
		s.logger.WarnContext(ctx, "pypi package endpoint failed", "alias", route.Alias, "attempt", i+1, "fallback", i+1 < len(urls))
	}
	return finishPyPIFetch(last, lastErr)
}

func (s *Service) packageAlternatives(ctx context.Context, req Request, route Route, cfg config.UpstreamConfig, initial []string) ([]string, string) {
	if route.DirectURL == "" {
		return nil, req.SHA256
	}
	wheelRoute := route
	if packageIsMetadata(route) {
		wheelRoute.DirectURL = strings.TrimSuffix(route.DirectURL, ".metadata")
	}
	if req.Project == "" || !validSHA256(req.SHA256) {
		req.Project, req.SHA256 = s.legacyPackageIdentity(ctx, cfg, wheelRoute)
	}
	if req.Project == "" || !validSHA256(req.SHA256) {
		return nil, req.SHA256
	}
	parsed, err := url.Parse(wheelRoute.DirectURL)
	if err != nil {
		return nil, req.SHA256
	}
	alternatives := s.resolvePackageMirrors(ctx, cfg, route.Alias, req.Project, path.Base(parsed.Path), req.SHA256)
	if packageIsMetadata(route) {
		alternatives = metadataAlternatives(alternatives)
	}
	return slices.DeleteFunc(alternatives, func(candidate string) bool { return slices.Contains(initial, candidate) }), req.SHA256
}

func packageIsMetadata(route Route) bool {
	return strings.HasSuffix(route.PackageTail, ".whl.metadata")
}

func metadataAlternatives(candidates []string) []string {
	for i, candidate := range candidates {
		parsed, err := url.Parse(candidate)
		if err == nil {
			parsed.Path += ".metadata"
			parsed.RawPath = ""
			candidates[i] = parsed.String()
		}
	}
	return candidates
}

func retryablePackageStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func packageHeaders(req Request) http.Header {
	headers := http.Header{"User-Agent": {defaultUserAgent}, "Accept-Encoding": {"identity"}}
	if methodOrGet(req.Method) == http.MethodGet && req.Range != "" {
		headers.Set("Range", req.Range)
		if req.IfRange != "" {
			headers.Set("If-Range", req.IfRange)
		}
	}
	return headers
}

func (s *Service) fetchPackageURL(ctx context.Context, req Request, cfg config.UpstreamConfig, rawURL string) (fetched *upstreamFetch, fetchErr error) {
	started := time.Now()
	defer func() { s.observePackageHeaders(ctx, req, rawURL, started, fetched, fetchErr) }()
	attemptTimeout := cfg.HTTP.AttemptTimeout
	if attemptTimeout <= 0 {
		attemptTimeout = pypiDefaultAttemptTimeout
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(attemptTimeout, cancel)
	auth := cfg.Auth
	if !sameOrigin(cfg.Registry, rawURL) {
		auth = config.AuthConfig{Type: "anonymous"}
	}
	response, err := s.doFetch(attemptCtx, cfg, rawURL, upstreamhttp.Request{Method: methodOrGet(req.Method), URL: rawURL, Headers: packageHeaders(req), Auth: auth})
	timer.Stop()
	if err == nil {
		err = attemptCtx.Err()
	}
	if err != nil {
		cancel()
		if response != nil {
			closeReadCloser(response.Body, s.logger, "close failed pypi response")
		}
		return nil, wrapError(err, "request pypi package headers")
	}
	if response.Status == http.StatusPartialContent || response.Status == http.StatusRequestedRangeNotSatisfiable {
		response.Headers.Set("Accept-Ranges", "bytes")
	}
	return &upstreamFetch{status: response.Status, headers: response.Headers, body: upstreamhttp.WithIdleTimeout(response.Body, cancel, pypiBodyIdleTimeout), requestURL: rawURL, transferContext: attemptCtx}, nil
}

func sameOrigin(left, right string) bool {
	a, aErr := url.Parse(left)
	b, bErr := url.Parse(right)
	return aErr == nil && bErr == nil && strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

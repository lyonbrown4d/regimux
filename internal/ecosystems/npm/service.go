package npm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	factory := deps.Factory
	if factory == nil {
		factory = clientfactory.New(logger)
	}
	cache := deps.Cache
	if cache == nil {
		cache = artifactcache.New(artifactcache.Dependencies{
			Metadata: deps.Metadata,
			Objects:  deps.Objects,
			Logger:   logger,
			Now:      now,
		})
	}
	fills := cache.FillTracker()
	if fills == nil {
		fills = artifactcache.NewFillTracker()
	}
	return &Service{
		cfg:         deps.Config,
		metadata:    deps.Metadata,
		cache:       cache,
		client:      deps.Client,
		factory:     factory,
		logger:      logger.With("component", "npm"),
		publicURL:   strings.TrimRight(deps.Config.Server.PublicURL, "/"),
		metadataTTL: deps.MetadataTTL,
		fills:       fills,
		now:         now,
		events:      deps.Events,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("npm").Errorf("service is nil")
	}
	requestRoute, err := parseRoute(req)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("npm").
			With("alias", requestRoute.Alias).
			Errorf("npm upstream is not configured")
	}
	requestRoute.MetadataTTL = s.metadataTTL
	if requestRoute.MetadataTTL <= 0 {
		requestRoute.MetadataTTL = upstreamCfg.TagTTL
	}
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeClient)
	s.recordPull(ctx, req, requestRoute, resp, err)
	return resp, err
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	ordered := s.cfg.OrderedNPMUpstreams()
	return collectionlist.NewList(lo.Map(ordered.Keys(), func(alias string, _ int) Upstream {
		cfg, _ := ordered.Get(alias)
		return Upstream{Alias: alias, Config: cfg}
	})...)
}

func (s *Service) getFromUpstream(
	ctx context.Context,
	req Request,
	requestRoute route,
	upstreamCfg config.UpstreamConfig,
	mode requestMode,
) (*Response, error) {
	if err := s.checkDependencyPolicy(requestRoute); err != nil {
		s.recordPolicyDeniedPull(ctx, req, requestRoute, err)
		return nil, err
	}
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	resp, cachedHit, cacheErr := s.responseFromCached(req, requestRoute, cached, cachedOK, mode)
	if cachedHit || cacheErr != nil {
		return resp, cacheErr
	}

	if shouldCoalesceFill(req, requestRoute, mode) {
		return artifactcache.CoalesceFill(ctx, s.fills, artifactKey(requestRoute), func() (*Response, bool, error) {
			cached, cachedOK, err := s.cached(ctx, requestRoute)
			if err != nil {
				return nil, true, err
			}
			resp, cachedHit, cacheErr := s.responseFromCached(req, requestRoute, cached, cachedOK, mode)
			if cachedHit || cacheErr != nil {
				return resp, true, cacheErr
			}
			return nil, false, nil
		}, func() (*Response, error) {
			fetched, err := s.fetch(ctx, upstreamCfg, requestRoute.Alias, requestRoute, req.Method)
			if err != nil {
				return s.responseFromFetchError(req, requestRoute, cached, cachedOK, err, mode)
			}
			return s.responseFromFetched(ctx, req, requestRoute, fetched)
		})
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute.Alias, requestRoute, req.Method)
	if err != nil {
		return s.responseFromFetchError(req, requestRoute, cached, cachedOK, err, mode)
	}
	return s.responseFromFetched(ctx, req, requestRoute, fetched)
}

func (s *Service) refresh(ctx context.Context, req Request) (*Response, error) {
	requestRoute, err := parseRoute(req)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("npm").
			With("alias", requestRoute.Alias).
			Errorf("npm upstream is not configured")
	}
	requestRoute.MetadataTTL = s.metadataTTL
	if requestRoute.MetadataTTL <= 0 {
		requestRoute.MetadataTTL = upstreamCfg.TagTTL
	}
	req.SkipPullRecord = true
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeRefresh)
}

func (s *Service) fetch(ctx context.Context, cfg config.UpstreamConfig, upstreamAlias string, requestRoute route, method string) (*upstreamFetch, error) {
	endpoints := ecosystem.UpstreamEndpoints(ctx, s.metadata, ecosystem.NPM, upstreamAlias, cfg)
	var lastErr error
	for _, endpoint := range endpoints {
		requestURL := strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(requestRoute.UpstreamTail, "/")
		requestURL = urlWithQuery(requestURL, requestRoute.Query)
		resp, err := s.fetchURL(ctx, cfg, requestURL, method)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no npm upstream endpoint is configured")
	}
	return nil, wrapError(lastErr, "fetch npm upstream")
}

func (s *Service) fetchURL(ctx context.Context, cfg config.UpstreamConfig, requestURL, method string) (*upstreamFetch, error) {
	req, err := http.NewRequestWithContext(ctx, requestMethod(method), requestURL, http.NoBody)
	if err != nil {
		return nil, wrapError(err, "create npm upstream request")
	}
	req.Header.Set("User-Agent", "regimux/dev")
	applyAuth(req, cfg.Auth)

	client, err := s.clientFor(cfg, requestURL)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, wrapError(err, "send npm upstream request")
	}
	defer closeReadCloser(resp.Body, s.logger, "close npm upstream response body")
	body, err := materializeBody(resp.Body)
	if err != nil {
		return nil, err
	}
	return &upstreamFetch{
		status:     resp.StatusCode,
		headers:    resp.Header.Clone(),
		body:       body,
		requestURL: requestURL,
	}, nil
}

func (s *Service) prepareFetched(req Request, requestRoute route, fetched *upstreamFetch) (*upstreamFetch, error) {
	if fetched == nil || fetched.body == nil {
		return fetched, nil
	}
	body, err := io.ReadAll(fetched.body)
	if err != nil {
		return nil, wrapError(err, "read npm upstream body")
	}
	closeReadCloser(fetched.body, s.logger, "close npm upstream body")

	if requestRoute.Kind != routeMetadata {
		return &upstreamFetch{
			status:     fetched.status,
			headers:    fetched.headers,
			body:       io.NopCloser(bytes.NewReader(body)),
			requestURL: fetched.requestURL,
		}, nil
	}
	rewritten, changed, err := rewriteMetadata(body, localBase(requestPublicURL(s.publicURL, req.ProxyBaseURL), requestRoute.Alias))
	if err != nil {
		return nil, err
	}
	if !changed {
		return &upstreamFetch{
			status:     fetched.status,
			headers:    fetched.headers,
			body:       io.NopCloser(bytes.NewReader(body)),
			requestURL: fetched.requestURL,
		}, nil
	}
	headers := fetched.headers.Clone()
	headers.Del("Content-Length")
	return &upstreamFetch{
		status:     fetched.status,
		headers:    headers,
		body:       io.NopCloser(bytes.NewReader(rewritten)),
		requestURL: fetched.requestURL,
	}, nil
}

func (s *Service) storeFetchedResponse(ctx context.Context, req Request, requestRoute route, fetched *upstreamFetch) (*Response, error) {
	stored, err := s.store(ctx, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	return s.responseFromStored(req, requestRoute, storedResponse{
		digest:  stored.digest,
		size:    stored.size,
		headers: stored.headers,
		body:    stored.body,
	}, cacheMiss)
}

func (s *Service) responseFromFetchError(req Request, requestRoute route, cached storedResponse, cachedOK bool, err error, mode requestMode) (*Response, error) {
	if mode == requestModeRefresh {
		return nil, err
	}
	if cachedOK {
		return s.responseFromStored(req, requestRoute, cached, cacheStale)
	}
	return nil, err
}

func (s *Service) responseFromStored(req Request, requestRoute route, stored storedResponse, cacheStatus string) (*Response, error) {
	headers := cacheHeaders(stored.headers, stored.size)
	headers.Set(headerMirrorCache, cacheStatus)
	body := stored.body
	if requestMethod(req.Method) == http.MethodHead && body != nil {
		if err := body.Close(); err != nil {
			return nil, wrapError(err, "close cached npm proxy object for head")
		}
		body = http.NoBody
	}
	return &Response{
		Status:      http.StatusOK,
		Headers:     headers,
		Body:        body,
		ContentType: contentType(requestRoute, headers),
		Size:        stored.size,
		Cache:       cacheStatus,
	}, nil
}

func (s *Service) responseFromUpstream(req Request, requestRoute route, fetched *upstreamFetch) *Response {
	headers := cacheHeaders(fetched.headers, -1)
	headers.Set(headerMirrorCache, cacheMiss)
	body := fetched.body
	if requestMethod(req.Method) == http.MethodHead && body != nil {
		closeReadCloser(body, s.logger, "close npm upstream body for head")
		body = http.NoBody
	}
	return &Response{
		Status:      fetched.status,
		Headers:     headers,
		Body:        body,
		ContentType: contentType(requestRoute, headers),
		Size:        contentLength(headers),
		Cache:       cacheMiss,
	}
}

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.NPMUpstream(alias)
}

func shouldPassThrough(req Request, status int) bool {
	return status < http.StatusOK ||
		status >= http.StatusMultipleChoices ||
		requestMethod(req.Method) == http.MethodHead
}

func shouldCoalesceFill(req Request, requestRoute route, mode requestMode) bool {
	return mode == requestModeClient &&
		requestMethod(req.Method) == http.MethodGet &&
		cacheable(requestRoute)
}

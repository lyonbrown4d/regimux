package pypi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
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
	return &Service{
		cfg:       deps.Config,
		cache:     cache,
		metadata:  deps.Metadata,
		client:    deps.Client,
		factory:   factory,
		logger:    logger.With("component", "pypi"),
		publicURL: strings.TrimRight(deps.Config.Server.PublicURL, "/"),
		fills:     artifactcache.NewFillTracker(),
		now:       now,
		events:    deps.Events,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("pypi").Errorf("service is nil")
	}
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("pypi").With("alias", requestRoute.Alias).Errorf("pypi upstream is not configured")
	}
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeClient)
	s.recordPull(ctx, req, requestRoute, resp, err)
	return resp, err
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute Route, upstreamCfg config.UpstreamConfig, mode requestMode) (*Response, error) {
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

	if shouldCoalesceFill(req, mode) {
		return s.getFromUpstreamWithFill(ctx, req, requestRoute, upstreamCfg, mode, cached, cachedOK)
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute.Alias, requestRoute, req.Method)
	if err != nil {
		return s.responseFromFetchError(req, requestRoute, cached, cachedOK, err, mode)
	}
	return s.responseFromFetched(ctx, req, requestRoute, fetched)
}

func (s *Service) responseFromCached(req Request, requestRoute Route, cached storedResponse, cachedOK bool, mode requestMode) (*Response, bool, error) {
	if mode == requestModeRefresh || !cachedOK {
		return nil, false, nil
	}
	if cached.expired {
		resp, err := s.responseFromStored(req, requestRoute, cached, cacheStale)
		return resp, true, err
	}
	resp, err := s.responseFromStored(req, requestRoute, cached, cacheHit)
	return resp, true, err
}

func (s *Service) responseFromFetchError(req Request, requestRoute Route, cached storedResponse, cachedOK bool, err error, mode requestMode) (*Response, error) {
	if mode == requestModeRefresh {
		return nil, err
	}
	if cachedOK {
		return s.responseFromStored(req, requestRoute, cached, cacheStale)
	}
	return nil, err
}

func (s *Service) responseFromFetched(ctx context.Context, req Request, requestRoute Route, fetched *upstreamFetch) (*Response, error) {
	if shouldPassThrough(req, fetched.status) {
		return s.responseFromUpstream(req, requestRoute, fetched), nil
	}
	prepared, err := s.prepareFetched(req, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	stored, err := s.store(ctx, requestRoute, prepared)
	if err != nil {
		return nil, err
	}
	return s.responseFromStored(req, requestRoute, stored, cacheMiss)
}

func (s *Service) refresh(ctx context.Context, req Request) (*Response, error) {
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("pypi").With("alias", requestRoute.Alias).Errorf("pypi upstream is not configured")
	}
	req.SkipPullRecord = true
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeRefresh)
}

func (s *Service) prepareFetched(req Request, requestRoute Route, fetched *upstreamFetch) (*upstreamFetch, error) {
	if fetched == nil || fetched.body == nil || requestRoute.Kind != RouteSimple {
		return fetched, nil
	}
	body, err := io.ReadAll(fetched.body)
	if err != nil {
		return nil, wrapError(err, "read pypi upstream body")
	}
	closeReadCloser(fetched.body, s.logger, "close pypi upstream body")

	rewritten := rewriteSimpleIndexLinks(body, requestRoute.Alias, fetched.requestURL, requestPublicURL(s.publicURL, req.PublicURL))
	if bytes.Equal(body, rewritten) {
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

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.PyPIUpstream(alias)
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	ordered := s.cfg.OrderedPyPIUpstreams()
	return collectionlist.NewList(lo.Map(ordered.Keys(), func(alias string, _ int) Upstream {
		cfg, _ := ordered.Get(alias)
		return Upstream{Alias: alias, Config: cfg}
	})...)
}

func (s *Service) upstreamSimpleTTL(alias string) time.Duration {
	cfg, ok := s.upstream(alias)
	if ok && cfg.TagTTL > 0 {
		return cfg.TagTTL
	}
	return defaultSimpleTTL
}

func routeTTL(requestRoute Route, simpleTTL time.Duration) time.Duration {
	if requestRoute.Kind != RouteSimple {
		return 0
	}
	if simpleTTL > 0 {
		return simpleTTL
	}
	return defaultSimpleTTL
}

func shouldCoalesceFill(req Request, mode requestMode) bool {
	return mode == requestModeClient && methodOrGet(req.Method) == http.MethodGet
}

func (s *Service) getFromUpstreamWithFill(
	ctx context.Context,
	req Request,
	requestRoute Route,
	upstreamCfg config.UpstreamConfig,
	mode requestMode,
	cached storedResponse,
	cachedOK bool,
) (*Response, error) {
	fillKey := artifactKey(requestRoute)
	for {
		fill, owner := s.fills.Begin(fillKey)
		if !owner {
			if resp, ok, err := s.waitForFill(ctx, req, requestRoute, fill, mode); ok || err != nil {
				return resp, err
			}
			continue
		}

		fetched, err := s.fetch(ctx, upstreamCfg, requestRoute.Alias, requestRoute, req.Method)
		if err != nil {
			s.fills.Finish(fillKey, fill, err)
			return s.responseFromFetchError(req, requestRoute, cached, cachedOK, err, mode)
		}
		resp, err := s.responseFromFetched(ctx, req, requestRoute, fetched)
		s.fills.Finish(fillKey, fill, err)
		return resp, err
	}
}

func (s *Service) waitForFill(ctx context.Context, req Request, requestRoute Route, fill *artifactcache.Fill, mode requestMode) (*Response, bool, error) {
	if err := fill.Wait(ctx); err != nil && ctx.Err() != nil {
		return nil, true, wrapError(ctx.Err(), "wait for pypi artifact cache fill")
	}
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, true, err
	}
	resp, cachedHit, cacheErr := s.responseFromCached(req, requestRoute, cached, cachedOK, mode)
	if cachedHit || cacheErr != nil {
		return resp, true, cacheErr
	}
	return nil, false, nil
}

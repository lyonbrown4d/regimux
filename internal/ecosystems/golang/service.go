package golang

import (
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
	"github.com/lyonbrown4d/regimux/internal/events"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/samber/oops"
)

const (
	defaultMetadataTTL = 5 * time.Minute

	headerMirrorCache = artifactcache.HeaderMirrorCache
	cacheHit          = artifactcache.CacheHit
	cacheMiss         = artifactcache.CacheMiss
	cacheStale        = artifactcache.CacheStale
)

type ServiceDependencies struct {
	Config   config.Config
	Cache    *artifactcache.Store
	Metadata meta.Store
	Objects  object.Store
	Factory  *clientfactory.Factory
	Logger   *slog.Logger
	Events   events.Bus
}

type Service struct {
	cfg      config.Config
	metadata meta.Store
	objects  object.Store
	factory  *clientfactory.Factory
	logger   *slog.Logger
	fills    *artifactcache.FillTracker
	events   events.Bus
}

type Request struct {
	Alias          string
	Tail           string
	Method         string
	SkipPullRecord bool
}

type Response struct {
	Status      int
	Headers     http.Header
	Body        io.ReadCloser
	ContentType string
	Size        int64
	Cache       string
}

type upstreamFetch struct {
	status  int
	headers http.Header
	body    io.ReadCloser
}

type Upstream struct {
	Alias  string
	Config config.UpstreamConfig
}

type goUpstream struct {
	alias string
	cfg   config.UpstreamConfig
}

type storedResponse struct {
	digest  string
	size    int64
	headers http.Header
	body    io.ReadCloser
	expired bool
}

type requestMode int

const (
	requestModeClient requestMode = iota
	requestModeRefresh
)

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
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
		})
	}
	metadata := deps.Metadata
	if metadata == nil {
		metadata = cache.Metadata()
	}
	objects := deps.Objects
	if objects == nil {
		objects = cache.Objects()
	}
	fills := cache.FillTracker()
	if fills == nil {
		fills = artifactcache.NewFillTracker()
	}
	return &Service{
		cfg:      deps.Config,
		metadata: metadata,
		objects:  objects,
		factory:  factory,
		logger:   logger.With("component", "go"),
		fills:    fills,
		events:   deps.Events,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("go").Errorf("service is nil")
	}
	if strings.TrimSpace(req.Alias) == "" {
		parsed, err := parseRootRoute(req.Tail)
		if err != nil {
			return nil, err
		}
		return s.getFromUpstreams(ctx, req, parsed, s.goUpstreams(), true, requestModeClient)
	}

	parsed, err := parseRoute(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.goUpstream(parsed.Alias)
	if !ok {
		return nil, oops.In("go").With("alias", parsed.Alias).Errorf("go upstream is not configured")
	}
	resp, err := s.getFromUpstreams(ctx, req, parsed, collectionlist.NewList(goUpstream{alias: parsed.Alias, cfg: upstreamCfg}), false, requestModeClient)
	s.recordPull(ctx, req, parsed, resp, err)
	return resp, err
}

func (s *Service) refresh(ctx context.Context, req Request) (*Response, error) {
	req.SkipPullRecord = true
	if strings.TrimSpace(req.Alias) == "" {
		parsed, err := parseRootRoute(req.Tail)
		if err != nil {
			return nil, err
		}
		return s.getFromUpstreams(ctx, req, parsed, s.goUpstreams(), true, requestModeRefresh)
	}
	parsed, err := parseRoute(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.goUpstream(parsed.Alias)
	if !ok {
		return nil, oops.In("go").With("alias", parsed.Alias).Errorf("go upstream is not configured")
	}
	return s.getFromUpstreams(ctx, req, parsed, collectionlist.NewList(goUpstream{alias: parsed.Alias, cfg: upstreamCfg}), false, requestModeRefresh)
}

func (s *Service) getFromUpstreams(ctx context.Context, req Request, baseRoute route, upstreams *collectionlist.List[goUpstream], fallback bool, mode requestMode) (*Response, error) {
	if upstreams == nil || upstreams.Len() == 0 {
		return nil, oops.In("go").Errorf("go upstream is not configured")
	}

	total := upstreams.Len()
	var lastErr error
	for i := range total {
		result, ok := s.tryGoUpstream(ctx, req, baseRoute, upstreams, upstreamAttempt{
			index:    i,
			total:    total,
			fallback: fallback,
			mode:     mode,
		})
		if !ok {
			continue
		}
		if result.stop {
			return result.resp, result.err
		}
		lastErr = result.err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, oops.In("go").Errorf("go upstream did not return module content")
}

type upstreamAttempt struct {
	index    int
	total    int
	fallback bool
	mode     requestMode
}

type upstreamAttemptResult struct {
	resp *Response
	err  error
	stop bool
}

func (s *Service) tryGoUpstream(
	ctx context.Context,
	req Request,
	baseRoute route,
	upstreams *collectionlist.List[goUpstream],
	attempt upstreamAttempt,
) (upstreamAttemptResult, bool) {
	upstream, ok := upstreams.Get(attempt.index)
	if !ok {
		return upstreamAttemptResult{}, false
	}
	requestRoute := routeForUpstream(baseRoute, upstream.alias)
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstream.cfg, upstream.alias, attempt.mode)
	if errors.Is(err, accesspolicy.ErrDependencyBlocked) {
		return upstreamAttemptResult{err: err, stop: true}, true
	}
	if s.shouldFallbackFromResponse(resp, err, attempt.fallback, attempt.index, attempt.total) {
		return upstreamAttemptResult{err: err}, true
	}
	return upstreamAttemptResult{resp: resp, err: err, stop: true}, true
}

func (s *Service) shouldFallbackFromResponse(resp *Response, err error, fallback bool, index, total int) bool {
	if err != nil {
		return canFallback(fallback, index, total)
	}
	if canFallbackResponse(resp, fallback, index, total) {
		closeResponseBody(resp)
		return true
	}
	return false
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute route, upstreamCfg config.UpstreamConfig, upstreamAlias string, mode requestMode) (*Response, error) {
	if err := s.checkDependencyPolicy(requestRoute); err != nil {
		s.recordPolicyDeniedPull(ctx, req, requestRoute, err)
		return nil, err
	}
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	if mode != requestModeRefresh && cacheFresh(cached, cachedOK) {
		return s.responseFromStored(req, cached, cacheHit)
	}
	if mode != requestModeRefresh && cachedOK && cached.expired {
		return s.responseFromStored(req, cached, cacheStale)
	}

	if shouldCoalesceFill(req, mode) {
		return artifactcache.CoalesceFill(ctx, s.fills, artifactKey(requestRoute), func() (*Response, bool, error) {
			cached, cachedOK, err := s.cached(ctx, requestRoute)
			if err != nil {
				return nil, true, err
			}
			if mode != requestModeRefresh && cacheFresh(cached, cachedOK) {
				resp, err := s.responseFromStored(req, cached, cacheHit)
				return resp, true, err
			}
			if mode != requestModeRefresh && cachedOK && cached.expired {
				resp, err := s.responseFromStored(req, cached, cacheStale)
				return resp, true, err
			}
			return nil, false, nil
		}, func() (*Response, error) {
			fetched, err := s.fetch(ctx, upstreamCfg, upstreamAlias, requestRoute, req.Method)
			if err != nil {
				return s.responseFromFetchError(req, cached, cachedOK, err, mode)
			}
			if shouldPassThrough(req, requestRoute, fetched.status) {
				return s.responseFromUpstream(req, fetched), nil
			}
			return s.storeFetchedResponse(ctx, req, requestRoute, fetched)
		})
	}

	fetched, err := s.fetch(ctx, upstreamCfg, upstreamAlias, requestRoute, req.Method)
	if err != nil {
		return s.responseFromFetchError(req, cached, cachedOK, err, mode)
	}
	if shouldPassThrough(req, requestRoute, fetched.status) {
		return s.responseFromUpstream(req, fetched), nil
	}
	return s.storeFetchedResponse(ctx, req, requestRoute, fetched)
}

func shouldCoalesceFill(req Request, mode requestMode) bool {
	return mode == requestModeClient && methodOr(req.Method, http.MethodGet) == http.MethodGet
}

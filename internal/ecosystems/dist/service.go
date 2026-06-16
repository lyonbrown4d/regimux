package dist

import (
	"context"
	"log/slog"
	"net/http"
	"path"
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
		cache:    cache,
		client:   deps.Client,
		factory:  factory,
		logger:   logger.With("component", "dist"),
		fills:    fills,
		now:      now,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("dist").Errorf("service is nil")
	}
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	upstreamCfg, allow, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("dist").With("alias", requestRoute.Alias).Errorf("dist upstream is not configured")
	}
	allowedErr := ensureAllowed(requestRoute.Tail, allow)
	if allowedErr != nil {
		s.recordPolicyDeniedPull(ctx, req, requestRoute, allowedErr)
		return nil, allowedErr
	}
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeClient)
	s.recordPull(ctx, req, requestRoute, resp, err)
	return resp, err
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute Route, upstreamCfg config.UpstreamConfig, mode requestMode) (*Response, error) {
	cached, cachedOK, err := s.cached(ctx, req, requestRoute)
	if err != nil {
		return nil, err
	}
	cachedResp, cacheOK, cacheErr := s.responseFromCache(req, requestRoute, cached, cachedOK, mode)
	if cacheOK || cacheErr != nil {
		return cachedResp, cacheErr
	}

	if shouldCoalesceFill(req, mode) {
		return artifactcache.CoalesceFill(ctx, s.fills, artifactKey(requestRoute), func() (*Response, bool, error) {
			cached, cachedOK, err := s.cached(ctx, req, requestRoute)
			if err != nil {
				return nil, true, err
			}
			resp, cacheOK, cacheErr := s.responseFromCache(req, requestRoute, cached, cachedOK, mode)
			if cacheOK || cacheErr != nil {
				return resp, true, cacheErr
			}
			return nil, false, nil
		}, func() (*Response, error) {
			fetched, err := s.fetch(ctx, upstreamCfg, requestRoute, req)
			if err != nil {
				return s.responseFromFetchError(req, requestRoute, cached, cachedOK, err, mode)
			}
			return s.responseFromFetched(ctx, req, requestRoute, fetched)
		})
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute, req)
	if err != nil {
		return s.responseFromFetchError(req, requestRoute, cached, cachedOK, err, mode)
	}
	return s.responseFromFetched(ctx, req, requestRoute, fetched)
}

func (s *Service) responseFromCache(req Request, requestRoute Route, cached storedResponse, cachedOK bool, mode requestMode) (*Response, bool, error) {
	if mode == requestModeRefresh || !cachedOK {
		return nil, false, nil
	}
	status := cacheHit
	if cached.expired {
		status = cacheStale
	}
	resp, err := s.responseFromStored(req, requestRoute, cached, status)
	return resp, true, err
}

func (s *Service) responseFromFetchError(req Request, requestRoute Route, cached storedResponse, cachedOK bool, err error, mode requestMode) (*Response, error) {
	if mode != requestModeRefresh && cachedOK {
		return s.responseFromStored(req, requestRoute, cached, cacheStale)
	}
	return nil, err
}

func (s *Service) responseFromFetched(ctx context.Context, req Request, requestRoute Route, fetched *upstreamFetch) (*Response, error) {
	if shouldPassThrough(req, fetched.status) {
		return s.responseFromUpstream(req, requestRoute, fetched), nil
	}
	stored, err := s.store(ctx, requestRoute, fetched)
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
	upstreamCfg, allow, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("dist").With("alias", requestRoute.Alias).Errorf("dist upstream is not configured")
	}
	if err := ensureAllowed(requestRoute.Tail, allow); err != nil {
		return nil, err
	}
	req.SkipPullRecord = true
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeRefresh)
}

func (s *Service) upstream(alias string) (config.UpstreamConfig, []string, bool) {
	upstreamCfg, ok := s.cfg.DistUpstream(alias)
	if !ok {
		return config.UpstreamConfig{}, nil, false
	}
	return upstreamCfg, s.cfg.DistAllow(alias), true
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	ordered := s.cfg.OrderedDistUpstreams()
	return collectionlist.NewList(lo.Map(ordered.Keys(), func(alias string, _ int) Upstream {
		cfg, _ := ordered.Get(alias)
		return Upstream{Alias: alias, Config: cfg, Allow: s.cfg.DistAllow(alias)}
	})...)
}

func (s *Service) artifactTTL(alias string) time.Duration {
	cfg, _, ok := s.upstream(alias)
	if ok && cfg.TagTTL > 0 {
		return cfg.TagTTL
	}
	return defaultMetadataTTL
}

func ensureAllowed(tail string, allow []string) error {
	if len(allow) == 0 {
		return nil
	}
	for _, pattern := range allow {
		matched, err := path.Match(strings.TrimSpace(pattern), tail)
		if err == nil && matched {
			return nil
		}
	}
	return oops.In("dist").With("path", tail).Wrapf(errBlockedPath, "dist path is not allowed")
}

func shouldPassThrough(req Request, status int) bool {
	if methodOrGet(req.Method) == http.MethodHead || strings.TrimSpace(req.Range) != "" {
		return true
	}
	return status < 200 || status >= 300
}

func shouldCoalesceFill(req Request, mode requestMode) bool {
	return mode == requestModeClient &&
		methodOrGet(req.Method) == http.MethodGet &&
		strings.TrimSpace(req.Range) == ""
}

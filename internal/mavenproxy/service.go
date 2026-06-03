package mavenproxy

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
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
	client := deps.Client
	if client == nil {
		client = &http.Client{}
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
		cfg:    deps.Config,
		cache:  cache,
		client: client,
		logger: logger.With("component", "maven-proxy"),
		now:    now,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("maven-proxy").Errorf("service is nil")
	}
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("maven-proxy").With("alias", requestRoute.Alias).Errorf("maven upstream is not configured")
	}
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg)
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute Route, upstreamCfg config.UpstreamConfig) (*Response, error) {
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	if cachedOK && !cached.expired {
		return s.responseFromStored(req, requestRoute, cached, cacheHit)
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute, req.Method)
	if err != nil {
		if cachedOK {
			return s.responseFromStored(req, requestRoute, cached, cacheStale)
		}
		return nil, err
	}
	if shouldPassThrough(req, fetched.status) {
		return s.responseFromUpstream(req, requestRoute, fetched), nil
	}
	stored, err := s.store(ctx, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	return s.responseFromStored(req, requestRoute, stored, cacheMiss)
}

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.MavenUpstream(alias)
}

func (s *Service) Upstreams() []Upstream {
	if s == nil {
		return nil
	}
	ordered := s.cfg.OrderedMavenUpstreams()
	out := make([]Upstream, 0, ordered.Len())
	ordered.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out = append(out, Upstream{Alias: alias, Config: cfg})
		return true
	})
	return out
}

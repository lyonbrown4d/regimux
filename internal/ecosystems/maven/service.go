package maven

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
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
		cfg:      deps.Config,
		cache:    cache,
		metadata: deps.Metadata,
		client:   client,
		logger:   logger.With("component", "maven"),
		now:      now,
		events:   deps.Events,
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("maven").Errorf("service is nil")
	}
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("maven").With("alias", requestRoute.Alias).Errorf("maven upstream is not configured")
	}
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeClient)
	s.recordPull(ctx, req, requestRoute, resp, err)
	return resp, err
}

func (s *Service) getFromUpstream(ctx context.Context, req Request, requestRoute Route, upstreamCfg config.UpstreamConfig, mode requestMode) (*Response, error) {
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	if mode != requestModeRefresh && cachedOK && !cached.expired {
		return s.responseFromStored(req, requestRoute, cached, cacheHit)
	}
	if mode != requestModeRefresh && cachedOK && cached.expired {
		return s.responseFromStored(req, requestRoute, cached, cacheStale)
	}

	fetched, err := s.fetch(ctx, upstreamCfg, requestRoute.Alias, requestRoute, req.Method)
	if err != nil {
		if mode == requestModeRefresh {
			return nil, err
		}
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

func (s *Service) refresh(ctx context.Context, req Request) (*Response, error) {
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	requestRoute.Query = strings.TrimSpace(req.Query)
	upstreamCfg, ok := s.upstream(requestRoute.Alias)
	if !ok {
		return nil, oops.In("maven").With("alias", requestRoute.Alias).Errorf("maven upstream is not configured")
	}
	req.SkipPullRecord = true
	return s.getFromUpstream(ctx, req, requestRoute, upstreamCfg, requestModeRefresh)
}

func (s *Service) recordPull(ctx context.Context, req Request, requestRoute Route, resp *Response, err error) {
	if !s.shouldRecordPull(req, resp, err) {
		return
	}
	key := mavenPullKey(requestRoute)
	s.recordPullKey(ctx, key, resp.Cache == cacheMiss)
	s.publishArtifactPulled(ctx, requestRoute, resp)
}

func (s *Service) shouldRecordPull(req Request, resp *Response, err error) bool {
	return s != nil &&
		s.metadata != nil &&
		!req.SkipPullRecord &&
		err == nil &&
		resp != nil &&
		resp.Status >= http.StatusOK &&
		resp.Status < http.StatusMultipleChoices
}

func mavenPullKey(requestRoute Route) meta.PullKey {
	return meta.PullKey{
		Alias:      ecosystem.ScopedAlias(ecosystem.Maven, requestRoute.Alias),
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
	}
}

func (s *Service) recordPullKey(ctx context.Context, key meta.PullKey, upstream bool) {
	now := s.now()
	if _, recordErr := s.metadata.RecordPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record maven proxy pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
	if !upstream {
		return
	}
	if _, recordErr := s.metadata.RecordUpstreamPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record maven proxy upstream pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
}

func (s *Service) publishArtifactPulled(ctx context.Context, requestRoute Route, resp *Response) {
	if s == nil || s.events == nil || resp == nil || requestRoute.Kind == RouteRelease {
		return
	}
	_ = events.Publish(ctx, s.events, events.ArtifactPulled{
		Ecosystem:  ecosystem.Maven,
		Kind:       string(requestRoute.Kind),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	})
}

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.MavenUpstream(alias)
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	ordered := s.cfg.OrderedMavenUpstreams()
	out := collectionlist.NewListWithCapacity[Upstream](ordered.Len())
	ordered.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out.Add(Upstream{Alias: alias, Config: cfg})
		return true
	})
	return out
}

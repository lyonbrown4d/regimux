package maven

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
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
		cfg:      deps.Config,
		cache:    cache,
		metadata: deps.Metadata,
		client:   deps.Client,
		factory:  factory,
		logger:   logger.With("component", "maven"),
		fills:    artifactcache.NewFillTracker(),
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
	if s.metadata != nil {
		s.recordPullKey(ctx, key, resp.Cache == cacheMiss)
	}
	s.publishDependencyPulled(ctx, requestRoute, resp)
	s.publishArtifactPulled(ctx, requestRoute, resp)
}

func (s *Service) shouldRecordPull(req Request, resp *Response, err error) bool {
	return s != nil &&
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
	if s == nil || s.metadata == nil {
		return
	}
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

func (s *Service) publishDependencyPulled(ctx context.Context, requestRoute Route, resp *Response) {
	if s == nil || s.events == nil || resp == nil {
		return
	}
	if err := events.Publish(ctx, s.events, events.DependencyPulled{
		Ecosystem:  ecosystem.Maven,
		Kind:       string(requestRoute.Kind),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish maven proxy dependency pulled event failed", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) recordPolicyDeniedPull(ctx context.Context, req Request, requestRoute Route, err error) {
	if s == nil ||
		req.SkipPullRecord ||
		!errors.Is(err, accesspolicy.ErrDependencyBlocked) {
		return
	}
	key := mavenPullKey(requestRoute)
	if s.metadata != nil {
		if _, recordErr := s.metadata.RecordPolicyDeniedPull(ctx, key, s.now()); recordErr != nil && s.logger != nil {
			s.logger.DebugContext(ctx, "record maven proxy policy denied pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
		}
	}
	s.publishDependencyPullDenied(ctx, requestRoute, err)
}

func (s *Service) publishDependencyPullDenied(ctx context.Context, requestRoute Route, denyErr error) {
	if s == nil || s.events == nil {
		return
	}
	reason := ""
	if denyErr != nil {
		reason = denyErr.Error()
	}
	if err := events.Publish(ctx, s.events, events.DependencyPullDenied{
		Ecosystem:  ecosystem.Maven,
		Kind:       string(requestRoute.Kind),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Reason:     reason,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish maven proxy policy denied pull event failed", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) publishArtifactPulled(ctx context.Context, requestRoute Route, resp *Response) {
	if s == nil || s.events == nil || resp == nil || requestRoute.Kind == RouteRelease {
		return
	}
	if err := events.Publish(ctx, s.events, events.ArtifactPulled{
		Ecosystem:  ecosystem.Maven,
		Kind:       string(requestRoute.Kind),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish maven proxy artifact pulled event failed", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) upstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.MavenUpstream(alias)
}

func (s *Service) Upstreams() *collectionlist.List[Upstream] {
	if s == nil {
		return collectionlist.NewList[Upstream]()
	}
	ordered := s.cfg.OrderedMavenUpstreams()
	return collectionlist.NewList(lo.Map(ordered.Keys(), func(alias string, _ int) Upstream {
		cfg, _ := ordered.Get(alias)
		return Upstream{Alias: alias, Config: cfg}
	})...)
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
		return nil, true, wrapError(ctx.Err(), "wait for maven artifact cache fill")
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

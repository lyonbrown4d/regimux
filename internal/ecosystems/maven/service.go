package maven

import (
	"context"
	"errors"
	"net/http"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/lo"
	"github.com/samber/oops"
)

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
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
	if shouldCoalesceFill(req, mode) {
		if pending := s.joinMavenFill(requestRoute); pending != nil {
			return s.waitMavenFill(ctx, req, requestRoute, pending, false)
		}
	}
	cached, cachedOK, err := s.cached(ctx, requestRoute)
	if err != nil {
		return nil, err
	}
	resp, cachedHit, cacheErr := s.responseFromCached(req, requestRoute, cached, cachedOK, mode)
	if cachedHit || cacheErr != nil {
		return resp, cacheErr
	}

	fillReq := upstreamFillRequest{
		ctx:      ctx,
		req:      req,
		route:    requestRoute,
		upstream: upstreamCfg,
		cached:   cached,
		cachedOK: cachedOK,
		mode:     mode,
	}
	if shouldCoalesceFill(req, mode) {
		return s.coalesceFill(fillReq)
	}

	return s.fetchUncached(fillReq)
}

type upstreamFillRequest struct {
	ctx      context.Context
	req      Request
	route    Route
	upstream config.UpstreamConfig
	cached   storedResponse
	cachedOK bool
	mode     requestMode
}

func (s *Service) fetchUncached(fillReq upstreamFillRequest) (*Response, error) {
	fetched, err := s.fetch(fillReq.ctx, fillReq.upstream, fillReq.route.Alias, fillReq.route, fillReq.req.Method)
	if err != nil {
		return s.responseFromFetchError(fillReq.req, fillReq.route, fillReq.cached, fillReq.cachedOK, err, fillReq.mode)
	}
	if fillReq.mode == requestModeRefresh {
		return s.responseFromFetchedSync(fillReq.ctx, fillReq.req, fillReq.route, fetched)
	}
	return s.responseFromFetched(fillReq.ctx, fillReq.req, fillReq.route, fetched)
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

// Background refresh waits for persistence, then serves the same validated replay.
func (s *Service) responseFromFetchedSync(ctx context.Context, req Request, requestRoute Route, fetched *upstreamFetch) (*Response, error) {
	if shouldPassThrough(req, fetched.status) {
		return s.responseFromFetched(ctx, req, requestRoute, fetched)
	}
	prepared, err := s.prepareMavenResponse(ctx, req, requestRoute, fetched)
	if err != nil {
		return nil, err
	}
	defer prepared.replay.Remove()
	if storeErr := s.persistPreparedMaven(ctx, requestRoute, prepared); storeErr != nil && ctx.Err() != nil {
		return nil, wrapError(ctx.Err(), "persist refreshed maven artifact")
	}
	return s.responseFromPrepared(ctx, req, requestRoute, prepared)
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
	if upstream, ok := s.cfg.MavenUpstream(alias); ok {
		return upstream, true
	}
	return s.groupUpstream(alias)
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

package maven

import (
	"context"
	"errors"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/panjf2000/ants/v2"
)

func (s *Service) coalesceFill(fillReq upstreamFillRequest) (*Response, error) {
	pool, err := s.mavenFillPool()
	if err != nil {
		return nil, err
	}
	pending, owner, err := pool.acquire(fillReq.ctx, artifactKey(fillReq.route), func(ctx context.Context, pending *pendingMavenFill) error {
		_, fillErr := s.fills.Coalesce(artifactcache.CoalesceRequest[struct{}]{
			Context: ctx, Key: artifactKey(fillReq.route),
			Wait: func() (struct{}, bool, error) {
				return s.prepareCachedFill(ctx, fillReq, pending)
			},
			Fill: func() (struct{}, error) {
				return struct{}{}, s.prepareUpstreamFill(ctx, fillReq, pending)
			},
		})
		return wrapError(fillErr, "coalesce maven cache fill")
	})
	if errors.Is(err, ants.ErrPoolOverload) {
		return s.fetchWithoutCache(fillReq)
	}
	if err != nil {
		return nil, wrapError(err, "schedule maven fill")
	}
	return s.waitMavenFill(fillReq.ctx, fillReq.req, fillReq.route, pending, owner)
}

func (s *Service) prepareCachedFill(ctx context.Context, req upstreamFillRequest, pending *pendingMavenFill) (struct{}, bool, error) {
	cached, ok, err := s.cached(ctx, req.route)
	if err != nil || !ok {
		return struct{}{}, false, err
	}
	prepared, err := s.prepareMavenResponse(ctx, req.req, req.route, &upstreamFetch{
		status: http.StatusOK, headers: cached.headers, body: cached.body,
	})
	if err != nil {
		return struct{}{}, false, err
	}
	prepared.cacheStatus = cacheHit
	if cached.expired {
		prepared.cacheStatus = cacheStale
	}
	if !pending.publish(prepared) {
		prepared.replay.Remove()
	}
	return struct{}{}, true, nil
}

func (s *Service) prepareUpstreamFill(ctx context.Context, req upstreamFillRequest, pending *pendingMavenFill) error {
	fetched, err := s.fetch(ctx, req.upstream, req.route.Alias, req.route, req.req.Method)
	if err != nil {
		return err
	}
	prepared, err := s.prepareMavenResponse(ctx, req.req, req.route, fetched)
	if err != nil {
		return err
	}
	if !pending.publish(prepared) {
		prepared.replay.Remove()
		return wrapError(ctx.Err(), "publish prepared maven fill")
	}
	return s.persistPreparedMaven(ctx, req.route, prepared)
}

func (s *Service) fetchWithoutCache(req upstreamFillRequest) (*Response, error) {
	if s.logger != nil {
		s.logger.WarnContext(req.ctx, "maven fill pool saturated; serving validated upstream without cache",
			"alias", req.route.Alias, "repository", req.route.Repository, "reference", req.route.Reference)
	}
	fetched, err := s.fetch(req.ctx, req.upstream, req.route.Alias, req.route, req.req.Method)
	if err != nil {
		return nil, err
	}
	prepared, err := s.prepareMavenResponse(req.ctx, req.req, req.route, fetched)
	if err != nil {
		return nil, err
	}
	defer prepared.replay.Remove()
	return s.responseFromPrepared(req.ctx, req.req, req.route, prepared)
}

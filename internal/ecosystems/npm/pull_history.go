package npm

import (
	"context"
	"errors"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) recordPull(ctx context.Context, req Request, requestRoute route, resp *Response, err error) {
	if !s.shouldRecordPull(req, requestRoute, resp, err) {
		return
	}
	key := npmPullKey(requestRoute)
	if s.metadata != nil {
		s.recordPullKey(ctx, key, resp.Cache == cacheMiss)
	}
	s.publishDependencyPulled(ctx, requestRoute, resp)
	s.publishArtifactPulled(ctx, requestRoute, resp)
}

func (s *Service) shouldRecordPull(req Request, requestRoute route, resp *Response, err error) bool {
	return s != nil &&
		!req.SkipPullRecord &&
		err == nil &&
		resp != nil &&
		cacheable(requestRoute) &&
		resp.Status >= http.StatusOK &&
		resp.Status < http.StatusMultipleChoices
}

func npmPullKey(requestRoute route) meta.PullKey {
	return meta.PullKey{
		Alias:      ecosystem.ScopedAlias(ecosystem.NPM, requestRoute.Alias),
		Repository: requestRoute.Package,
		Reference:  requestRoute.Reference,
	}
}

func (s *Service) recordPullKey(ctx context.Context, key meta.PullKey, upstream bool) {
	if s == nil || s.metadata == nil {
		return
	}
	now := s.now()
	if _, recordErr := s.metadata.RecordPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record npm proxy pull failed", "alias", key.Alias, "package", key.Repository, "reference", key.Reference, "error", recordErr)
	}
	if !upstream {
		return
	}
	if _, recordErr := s.metadata.RecordUpstreamPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record npm proxy upstream pull failed", "alias", key.Alias, "package", key.Repository, "reference", key.Reference, "error", recordErr)
	}
}

func (s *Service) publishDependencyPulled(ctx context.Context, requestRoute route, resp *Response) {
	if s == nil || s.events == nil || resp == nil {
		return
	}
	if err := events.Publish(ctx, s.events, events.DependencyPulled{
		Ecosystem:  ecosystem.NPM,
		Kind:       requestRoute.Kind,
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Package,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish npm proxy dependency pulled event failed", "alias", requestRoute.Alias, "package", requestRoute.Package, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) recordPolicyDeniedPull(ctx context.Context, req Request, requestRoute route, err error) {
	if s == nil ||
		req.SkipPullRecord ||
		!errors.Is(err, accesspolicy.ErrDependencyBlocked) ||
		!cacheable(requestRoute) {
		return
	}
	key := npmPullKey(requestRoute)
	if s.metadata != nil {
		if _, recordErr := s.metadata.RecordPolicyDeniedPull(ctx, key, s.now()); recordErr != nil && s.logger != nil {
			s.logger.DebugContext(ctx, "record npm proxy policy denied pull failed", "alias", key.Alias, "package", key.Repository, "reference", key.Reference, "error", recordErr)
		}
	}
	s.publishDependencyPullDenied(ctx, requestRoute, err)
}

func (s *Service) publishDependencyPullDenied(ctx context.Context, requestRoute route, denyErr error) {
	if s == nil || s.events == nil {
		return
	}
	reason := ""
	if denyErr != nil {
		reason = denyErr.Error()
	}
	if err := events.Publish(ctx, s.events, events.DependencyPullDenied{
		Ecosystem:  ecosystem.NPM,
		Kind:       requestRoute.Kind,
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Package,
		Reference:  requestRoute.Reference,
		Reason:     reason,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish npm proxy policy denied pull event failed", "alias", requestRoute.Alias, "package", requestRoute.Package, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) publishArtifactPulled(ctx context.Context, requestRoute route, resp *Response) {
	if s == nil || s.events == nil || resp == nil || requestRoute.Kind != routeMetadata {
		return
	}
	if err := events.Publish(ctx, s.events, events.ArtifactPulled{
		Ecosystem:  ecosystem.NPM,
		Kind:       routeMetadata,
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Package,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish npm proxy artifact pulled event failed", "alias", requestRoute.Alias, "package", requestRoute.Package, "reference", requestRoute.Reference, "error", err)
	}
}

package pypi

import (
	"context"
	"errors"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/events"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) recordPull(ctx context.Context, req Request, requestRoute Route, resp *Response, err error) {
	if !s.shouldRecordPull(req, resp, err) {
		return
	}
	key := pypiPullKey(requestRoute)
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

func pypiPullKey(requestRoute Route) meta.PullKey {
	return meta.PullKey{
		Alias:      ecosystem.ScopedAlias(ecosystem.PyPI, requestRoute.Alias),
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
		s.logger.DebugContext(ctx, "record pypi proxy pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
	if !upstream {
		return
	}
	if _, recordErr := s.metadata.RecordUpstreamPull(ctx, key, now); recordErr != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "record pypi proxy upstream pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
	}
}

func (s *Service) publishDependencyPulled(ctx context.Context, requestRoute Route, resp *Response) {
	if s == nil || s.events == nil || resp == nil {
		return
	}
	if err := events.Publish(ctx, s.events, events.DependencyPulled{
		Ecosystem:  ecosystem.PyPI,
		Kind:       string(requestRoute.Kind),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish pypi proxy dependency pulled event failed", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) recordPolicyDeniedPull(ctx context.Context, req Request, requestRoute Route, err error) {
	if s == nil ||
		req.SkipPullRecord ||
		!errors.Is(err, accesspolicy.ErrDependencyBlocked) {
		return
	}
	key := pypiPullKey(requestRoute)
	if s.metadata != nil {
		if _, recordErr := s.metadata.RecordPolicyDeniedPull(ctx, key, s.now()); recordErr != nil && s.logger != nil {
			s.logger.DebugContext(ctx, "record pypi proxy policy denied pull failed", "alias", key.Alias, "repository", key.Repository, "reference", key.Reference, "error", recordErr)
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
		Ecosystem:  ecosystem.PyPI,
		Kind:       string(requestRoute.Kind),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Reason:     reason,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish pypi proxy policy denied pull event failed", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
	}
}

func (s *Service) publishArtifactPulled(ctx context.Context, requestRoute Route, resp *Response) {
	if s == nil || s.events == nil || resp == nil || requestRoute.Kind != RouteSimple {
		return
	}
	if err := events.Publish(ctx, s.events, events.ArtifactPulled{
		Ecosystem:  ecosystem.PyPI,
		Kind:       string(RouteSimple),
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
		Status:     resp.Cache,
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish pypi proxy artifact pulled event failed", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
	}
}

package npmproxy

import (
	"context"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) recordPull(ctx context.Context, req Request, requestRoute route, resp *Response, err error) {
	if !s.shouldRecordPull(req, requestRoute, resp, err) {
		return
	}
	key := npmPullKey(requestRoute)
	s.recordPullKey(ctx, key, resp.Cache == cacheMiss)
}

func (s *Service) shouldRecordPull(req Request, requestRoute route, resp *Response, err error) bool {
	return s != nil &&
		s.metadata != nil &&
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

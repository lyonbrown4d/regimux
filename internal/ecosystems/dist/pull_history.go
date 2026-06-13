package dist

import (
	"context"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) recordPull(ctx context.Context, req Request, requestRoute Route, resp *Response, err error) {
	if s == nil || s.metadata == nil || req.SkipPullRecord || err != nil || resp == nil || resp.Status >= http.StatusBadRequest {
		return
	}
	key := meta.PullKey{Alias: requestRoute.Alias, Repository: requestRoute.Repository, Reference: requestRoute.Reference}
	if _, recordErr := s.metadata.RecordPull(ctx, key, s.now()); recordErr != nil {
		s.logger.Warn("record dist pull failed", "error", recordErr)
	}
	if resp.Cache == cacheMiss {
		if _, recordErr := s.metadata.RecordUpstreamPull(ctx, key, s.now()); recordErr != nil {
			s.logger.Warn("record dist upstream pull failed", "error", recordErr)
		}
	}
}

func (s *Service) recordPolicyDeniedPull(ctx context.Context, req Request, requestRoute Route, err error) {
	if s == nil || s.metadata == nil || req.SkipPullRecord || err == nil {
		return
	}
	if _, recordErr := s.metadata.RecordPolicyDeniedPull(ctx, meta.PullKey{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
	}, s.now()); recordErr != nil {
		s.logger.Warn("record dist denied pull failed", "error", recordErr)
	}
}

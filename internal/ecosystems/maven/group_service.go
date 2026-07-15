package maven

import (
	"context"
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/config"
)

// GetGroup resolves a request through a logical Maven group.
func (s *Service) GetGroup(ctx context.Context, req Request) (*Response, error) {
	requestRoute, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	upstream, ok := s.groupUpstream(requestRoute.Alias)
	if !ok {
		return nil, fmt.Errorf("resolve Maven group %q: group is not configured", requestRoute.Alias)
	}
	resp, err := s.getFromUpstream(ctx, req, requestRoute, upstream, requestModeClient)
	s.recordPull(ctx, req, requestRoute, resp, err)
	return resp, err
}

func (s *Service) physicalUpstream(alias string) (config.UpstreamConfig, bool) {
	return s.cfg.MavenUpstream(alias)
}

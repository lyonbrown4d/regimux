package maven

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

func (s *Service) cached(ctx context.Context, requestRoute Route) (storedResponse, bool, error) {
	if s.cache == nil {
		return storedResponse{}, false, nil
	}
	entry, ok, err := s.cache.Get(ctx, artifactKey(requestRoute))
	if err != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "maven cache lookup failed; falling back to upstream", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error", err)
		}
		return storedResponse{}, false, nil
	}
	if !ok {
		return storedResponse{}, false, nil
	}
	return storedResponse{
		digest:  entry.Digest,
		size:    entry.Size,
		headers: entry.Headers,
		body:    entry.Body,
		expired: entry.Expired,
	}, true, nil
}

func artifactKey(requestRoute Route) artifactcache.Key {
	return artifactcache.Key{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Repository,
		Reference:  requestRoute.Reference,
	}
}

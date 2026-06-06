package npm

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/samber/oops"
)

func (s *Service) cached(ctx context.Context, requestRoute route) (storedResponse, bool, error) {
	if s.cache == nil {
		return storedResponse{}, false, nil
	}
	entry, ok, err := s.cache.Get(ctx, artifactKey(requestRoute))
	if err != nil {
		return storedResponse{}, false, wrapError(err, "lookup cached npm artifact")
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

func (s *Service) store(ctx context.Context, requestRoute route, fetched *upstreamFetch) (storedResponse, error) {
	if s.cache == nil {
		return storedResponse{}, oops.In("npm").Errorf("npm proxy cache store is not configured")
	}
	if fetched == nil || fetched.body == nil {
		return storedResponse{}, oops.In("npm").Errorf("npm upstream body is empty")
	}
	defer closeReadCloser(fetched.body, s.logger, "close npm upstream body")
	entry, err := s.cache.Put(ctx, artifactcache.PutRequest{
		Key:         artifactKey(requestRoute),
		AcceptKey:   ecosystemNPM,
		Body:        fetched.body,
		Headers:     fetched.headers,
		ContentType: contentType(requestRoute, fetched.headers),
		TTL:         routeTTL(requestRoute, requestRoute.MetadataTTL),
	})
	if err != nil {
		return storedResponse{}, wrapError(err, "store npm artifact")
	}
	return storedResponse{
		digest:  entry.Digest,
		size:    entry.Size,
		headers: entry.Headers,
		body:    entry.Body,
	}, nil
}

func artifactKey(requestRoute route) artifactcache.Key {
	return artifactcache.Key{
		Alias:      requestRoute.Alias,
		Repository: requestRoute.Package,
		Reference:  requestRoute.Reference,
	}
}

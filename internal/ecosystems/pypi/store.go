package pypi

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/samber/oops"
)

func (s *Service) store(ctx context.Context, requestRoute Route, fetched *upstreamFetch) (storedResponse, error) {
	if s.cache == nil {
		return storedResponse{}, oops.In("pypi").Errorf("pypi cache store is not configured")
	}
	if fetched == nil || fetched.body == nil {
		return storedResponse{}, oops.In("pypi").Errorf("pypi upstream body is empty")
	}
	defer closeReadCloser(fetched.body, s.logger, "close pypi upstream body")
	entry, err := s.cache.Put(ctx, artifactcache.PutRequest{
		Key:         artifactKey(requestRoute),
		AcceptKey:   acceptKeyPyPI,
		Body:        fetched.body,
		Headers:     fetched.headers,
		ContentType: routeContentType(requestRoute, fetched.headers),
		TTL:         routeTTL(requestRoute, s.upstreamSimpleTTL(requestRoute.Alias)),
	})
	if err != nil {
		return storedResponse{}, wrapError(err, "store pypi artifact")
	}
	return storedResponse{
		digest:  entry.Digest,
		size:    entry.Size,
		headers: entry.Headers,
		body:    entry.Body,
	}, nil
}

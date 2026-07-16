package dist

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/samber/oops"
)

func (s *Service) store(ctx context.Context, requestRoute Route, fetched *upstreamFetch) (storedResponse, error) {
	if s.cache == nil {
		return storedResponse{}, oops.In("dist").Errorf("dist cache store is not configured")
	}
	if fetched == nil || fetched.body == nil {
		return storedResponse{}, oops.In("dist").Errorf("dist upstream body is empty")
	}
	defer closeReadCloser(fetched.body, s.logger, "close dist upstream body")
	entry, err := s.cache.Put(ctx, artifactcache.PutRequest{
		Key:         artifactKey(requestRoute),
		AcceptKey:   acceptKeyDist,
		Body:        fetched.body,
		Headers:     fetched.headers,
		ContentType: routeContentType(requestRoute, fetched.headers),
		TTL:         s.artifactTTL(requestRoute.Alias),
		Validator:   distBodyValidator(requestRoute),
	})
	if err != nil {
		return storedResponse{}, wrapError(err, "store dist artifact")
	}
	return storedResponse{
		digest:  entry.Digest,
		size:    entry.Size,
		headers: entry.Headers,
		body:    entry.Body,
		status:  200,
	}, nil
}

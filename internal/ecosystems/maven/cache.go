package maven

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/transfer"
)

const defaultMavenCacheReadTimeout = time.Second

func mavenCacheReadContext(parent context.Context) (context.Context, func(), context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	timer := time.AfterFunc(defaultMavenCacheReadTimeout, cancel)
	// Stop the open timeout once Get returns so a streamed object body can be read;
	// the body wrapper owns cancellation until Close.
	return ctx, func() { timer.Stop() }, cancel
}

//nolint:cyclop,funlen,gocognit,gocyclo // Lookup timeout, fallback, and body ownership form one operation.
func (s *Service) cached(ctx context.Context, requestRoute Route) (storedResponse, bool, error) {
	if s.cache == nil {
		s.publishCacheAccess(ctx, requestRoute, cacheMiss, "")
		return storedResponse{}, false, nil
	}
	readCtx, stopTimeout, cancel := mavenCacheReadContext(ctx)
	entry, ok, err := s.cache.Get(readCtx, artifactKey(requestRoute))
	if err != nil {
		s.publishCacheAccess(ctx, requestRoute, "error", "")
		stopTimeout()
		cancel()
		if ctx.Err() != nil {
			return storedResponse{}, false, wrapError(ctx.Err(), "read maven cache")
		}
		if s.logger != nil {
			s.logger.WarnContext(ctx, "maven cache lookup failed; falling back to upstream", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error_kind", mavenErrorKind(err))
		}
		return storedResponse{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		stopTimeout()
		cancel()
		closeReadCloser(entry.Body, s.logger, "close canceled maven cache response")
		return storedResponse{}, false, wrapError(err, "read maven cache")
	}
	if readErr := readCtx.Err(); readErr != nil {
		s.publishCacheAccess(ctx, requestRoute, "error", "")
		stopTimeout()
		cancel()
		closeReadCloser(entry.Body, s.logger, "close timed out maven cache response")
		if s.logger != nil {
			s.logger.WarnContext(ctx, "maven cache lookup timed out; falling back to upstream", "alias", requestRoute.Alias, "repository", requestRoute.Repository, "reference", requestRoute.Reference, "error_kind", mavenErrorKind(readErr))
		}
		return storedResponse{}, false, nil
	}
	if !ok {
		s.publishCacheAccess(ctx, requestRoute, cacheMiss, "")
		stopTimeout()
		cancel()
		closeReadCloser(entry.Body, s.logger, "close incomplete cached maven artifact")
		return storedResponse{}, false, nil
	}
	stopTimeout()
	if entry.Body != nil {
		entry.Body = transfer.CancelOnClose(entry.Body, cancel)
	} else {
		cancel()
	}
	status := cacheHit
	if entry.Expired {
		status = cacheStale
	}
	s.publishCacheAccess(ctx, requestRoute, status, entry.Digest)
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

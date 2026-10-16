package pypi

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const pypiSimpleIndexFormat = "pypi:2"

func (s *Service) cached(ctx context.Context, requestRoute Route) (storedResponse, bool, error) {
	if s.cache == nil {
		return storedResponse{}, false, nil
	}
	cacheCtx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(pypiCacheTimeout, cancel)
	entry, ok, err := s.cache.Get(cacheCtx, artifactKey(requestRoute))
	timer.Stop()
	if err != nil {
		cancel()
		if entry.Body != nil {
			closeReadCloser(entry.Body, s.logger, "close failed pypi index cache")
		}
		if ctx.Err() != nil {
			return storedResponse{}, false, wrapError(ctx.Err(), "lookup pypi index cache")
		}
		s.logger.WarnContext(ctx, "pypi index cache read failed; fetching upstream", "alias", requestRoute.Alias)
		return storedResponse{}, false, nil
	}
	if !ok {
		cancel()
		return storedResponse{}, false, nil
	}
	if requestRoute.Kind == RouteSimple && entry.Headers.Get("X-Regimux-Metadata-Format") != pypiSimpleIndexFormat {
		cancel()
		closeReadCloser(entry.Body, s.logger, "close incompatible cached pypi simple index")
		return storedResponse{}, false, nil
	}
	entry.Body = upstreamhttp.WithIdleTimeout(entry.Body, cancel, pypiBodyIdleTimeout)
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

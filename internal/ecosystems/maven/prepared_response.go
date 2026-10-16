package maven

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/panjf2000/ants/v2"
)

const defaultMavenFillConcurrency = 8

type preparedMavenResponse struct {
	replay      *replayableBody
	fetched     upstreamFetch
	cacheable   bool
	cacheStatus string
}

func (s *Service) joinMavenFill(route Route) *pendingMavenFill {
	s.clientMu.Lock()
	pool := s.fillPool
	s.clientMu.Unlock()
	if pool == nil {
		return nil
	}
	return pool.join(artifactKey(route))
}

func (s *Service) mavenFillPool() (*mavenFillPool, error) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if s.closed {
		return nil, errors.New("maven service is closed")
	}
	if s.fillPool == nil {
		pool, err := newMavenFillPool(defaultMavenFillConcurrency)
		if err != nil {
			return nil, wrapError(err, "create maven fill pool")
		}
		s.fillPool = pool
	}
	return s.fillPool, nil
}

func (s *Service) prepareMavenResponse(ctx context.Context, req Request, route Route, fetched *upstreamFetch) (*preparedMavenResponse, error) {
	if err := ctx.Err(); err != nil {
		closeReadCloser(fetched.body, s.logger, "close canceled maven response")
		return nil, wrapError(err, "prepare maven response")
	}
	started := time.Now()
	replay, err := newReplayableBody(ctx, fetched.body)
	if err != nil {
		return nil, err
	}
	cacheable := !shouldPassThrough(req, fetched.status)
	if cacheable {
		if validationErr := validateMavenReplay(route, fetched, replay); validationErr != nil {
			replay.Remove()
			return nil, wrapError(validationErr, "validate maven upstream artifact")
		}
	}
	if contextErr := ctx.Err(); contextErr != nil {
		replay.Remove()
		return nil, wrapError(contextErr, "prepare maven response")
	}
	info, err := os.Stat(replay.path)
	if err != nil {
		replay.Remove()
		return nil, wrapError(err, "stat maven replay")
	}
	headers := cacheHeaders(fetched.headers, info.Size())
	if s.logger != nil {
		s.logger.DebugContext(ctx, "maven replay ready", "alias", route.Alias, "repository", route.Repository,
			"reference", route.Reference, "bytes", info.Size(), "duration", time.Since(started))
	}
	return &preparedMavenResponse{
		replay: replay, fetched: upstreamFetch{status: fetched.status, headers: headers},
		cacheable: cacheable, cacheStatus: cacheMiss,
	}, nil
}

func (s *Service) responseFromPrepared(ctx context.Context, req Request, route Route, prepared *preparedMavenResponse) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, wrapError(err, "serve prepared maven response")
	}
	body, err := prepared.replay.Open(false)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		closeReadCloser(body, s.logger, "close canceled maven replay")
		return nil, wrapError(err, "serve prepared maven response")
	}
	fetched := prepared.fetched
	fetched.body = body
	response := s.responseFromUpstream(req, route, &fetched)
	response.Cache = prepared.cacheStatus
	response.Headers.Set(headerMirrorCache, prepared.cacheStatus)
	return response, nil
}

func (s *Service) waitMavenFill(ctx context.Context, req Request, route Route, pending *pendingMavenFill, owner bool) (*Response, error) {
	defer pending.release()
	started := time.Now()
	prepared, err := pending.wait(ctx)
	if s.logger != nil {
		s.logger.DebugContext(ctx, "maven fill wait finished", "alias", route.Alias, "repository", route.Repository,
			"reference", route.Reference, "duration", time.Since(started), "error_kind", mavenErrorKind(err))
	}
	if err != nil {
		return nil, wrapError(err, "wait for prepared maven artifact")
	}
	responseCopy := *prepared
	if !owner && responseCopy.cacheable && responseCopy.cacheStatus == cacheMiss {
		// The complete validated local replay is already available to this waiter.
		responseCopy.cacheStatus = cacheHit
	}
	return s.responseFromPrepared(ctx, req, route, &responseCopy)
}

func (s *Service) persistPreparedMaven(ctx context.Context, route Route, prepared *preparedMavenResponse) error {
	if s.cache == nil || !prepared.cacheable {
		return nil
	}
	body, err := prepared.replay.Open(false)
	if err != nil {
		return err
	}
	defer closeReadCloser(body, s.logger, "close maven cache write replay")
	writeCtx, cancel := context.WithTimeout(ctx, defaultMavenCacheWriteTimeout)
	defer cancel()
	started := time.Now()
	entry, err := s.cache.Put(writeCtx, artifactcache.PutRequest{
		Key: artifactKey(route), AcceptKey: acceptKeyMaven,
		Body: contextReader{ctx: writeCtx, reader: body}, Headers: prepared.fetched.headers,
		ContentType: routeContentType(route, prepared.fetched.headers), TTL: routeTTL(route),
		Validator: mavenBodyValidator(route),
	})
	closeReadCloser(entry.Body, s.logger, "close maven cache write result")
	if err == nil {
		s.publishCacheStore(writeCtx, route, entry.Digest, entry.Size)
	}
	if s.logger != nil {
		if err != nil {
			s.logger.WarnContext(writeCtx, "maven cache write failed; validated replay remains available",
				"alias", route.Alias, "repository", route.Repository, "reference", route.Reference,
				"duration", time.Since(started), "error_kind", mavenErrorKind(err))
		} else {
			s.logger.DebugContext(writeCtx, "maven cache write finished", "alias", route.Alias,
				"repository", route.Repository, "reference", route.Reference, "duration", time.Since(started))
		}
	}
	return wrapError(err, "persist prepared maven artifact")
}

// Direct callers also hand cache persistence to the bounded fill pool.
//
//nolint:cyclop,gocognit,gocyclo // Replay ownership transfers on admission, join, or overload.
func (s *Service) responseFromFetched(ctx context.Context, req Request, route Route, fetched *upstreamFetch) (*Response, error) {
	if err := ctx.Err(); err != nil {
		closeReadCloser(fetched.body, s.logger, "close canceled maven response")
		return nil, wrapError(err, "prepare maven response")
	}
	if shouldPassThrough(req, fetched.status) {
		return s.responseFromUpstream(req, route, fetched), nil
	}
	prepared, err := s.prepareMavenResponse(ctx, req, route, fetched)
	if err != nil {
		return nil, err
	}
	if s.cache == nil || !prepared.cacheable {
		defer prepared.replay.Remove()
		return s.responseFromPrepared(ctx, req, route, prepared)
	}
	pool, err := s.mavenFillPool()
	if err != nil {
		prepared.replay.Remove()
		return nil, err
	}
	pending, owner, err := pool.acquire(ctx, artifactKey(route), func(workCtx context.Context, pending *pendingMavenFill) error {
		if !pending.publish(prepared) {
			prepared.replay.Remove()
			return workCtx.Err()
		}
		return s.persistPreparedMaven(workCtx, route, prepared)
	})
	if !owner || err != nil {
		if errors.Is(err, ants.ErrPoolOverload) {
			defer prepared.replay.Remove()
			return s.responseFromPrepared(ctx, req, route, prepared)
		}
		prepared.replay.Remove()
	}
	if err != nil {
		return nil, err
	}
	return s.waitMavenFill(ctx, req, route, pending, owner)
}

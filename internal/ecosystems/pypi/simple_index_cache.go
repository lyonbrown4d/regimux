package pypi

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/spool"
)

func (s *Service) responseFromPreparedSimpleIndex(
	ctx context.Context,
	req Request,
	route Route,
	fetched *upstreamFetch,
) (*Response, error) {
	if fetched == nil || fetched.body == nil {
		if fetched == nil {
			return nil, errors.New("pypi simple index fetch is nil")
		}
		return s.responseFromUpstream(req, route, fetched), nil
	}
	tmp, ok := fetched.body.(*spool.TempBody)
	if !ok {
		closeReadCloser(fetched.body, s.logger, "close unexpected pypi simple index body")
		return nil, errors.New("pypi simple index body is not spooled")
	}
	serveFromTemp := func() (*Response, error) {
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			closeReadCloser(tmp, s.logger, "close unreadable pypi simple index spool")
			return nil, wrapError(err, "rewind pypi simple index spool")
		}
		fetched.body = tmp
		return s.responseFromUpstream(req, route, fetched), nil
	}
	if s.cache == nil {
		s.logger.WarnContext(ctx, "pypi index cache write skipped; serving upstream", "alias", route.Alias)
		return serveFromTemp()
	}
	return s.cachePreparedSimpleIndex(ctx, route, fetched, tmp, serveFromTemp)
}

func (s *Service) cachePreparedSimpleIndex(
	ctx context.Context,
	route Route,
	fetched *upstreamFetch,
	tmp *spool.TempBody,
	serveFromTemp func() (*Response, error),
) (*Response, error) {
	writeCtx, cancel := context.WithTimeout(ctx, pypiCacheWriteTimeout)
	entry, err := s.cache.Put(writeCtx, artifactcache.PutRequest{
		Key:         artifactKey(route),
		AcceptKey:   acceptKeyPyPI,
		Body:        tmp,
		Headers:     fetched.headers,
		ContentType: routeContentType(route, fetched.headers),
		TTL:         routeTTL(route, s.upstreamSimpleTTL(route.Alias)),
	})
	cancel()
	if ctx.Err() != nil {
		closeReadCloser(entry.Body, s.logger, "close canceled pypi index cache result")
		closeReadCloser(tmp, s.logger, "close canceled pypi index spool")
		return nil, wrapError(ctx.Err(), "prepare pypi index response")
	}
	if err != nil {
		closeReadCloser(entry.Body, s.logger, "close failed pypi index cache result")
		s.logger.WarnContext(ctx, "pypi index cache write failed; serving upstream", "alias", route.Alias)
		return serveFromTemp()
	}
	if entry.Body == nil {
		closeReadCloser(tmp, s.logger, "close cached pypi index without body")
		return nil, errors.New("pypi index cache returned an empty body")
	}
	// Store.Put opens its returned body with writeCtx. That context is canceled
	// above because it only bounds the cache write, so serving entry.Body would
	// make every subsequent read fail with context canceled. The spool contains
	// the exact bytes submitted to Put and remains owned by the response body.
	closeReadCloser(entry.Body, s.logger, "close cached pypi index object")
	return serveFromTemp()
}

func materializeSimpleIndexBody(body []byte) (*spool.TempBody, error) {
	tmp, err := spool.NewTempBody("regimux-pypi-index-*")
	if err != nil {
		return nil, fmt.Errorf("create pypi simple index spool: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		return nil, errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Join(err, tmp.Close())
	}
	return tmp, nil
}

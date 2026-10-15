package maven

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/samber/oops"
)

const defaultMavenCacheWriteTimeout = 30 * time.Second

type replayableBody struct {
	path string
	once sync.Once
}

func newReplayableBody(body io.ReadCloser) (*replayableBody, error) {
	if body == nil {
		return nil, oops.In("maven").Errorf("maven upstream body is empty")
	}
	tmp, err := os.CreateTemp("", "regimux-maven-replay-*")
	if err != nil {
		return nil, wrapError(err, "create maven upstream replay body")
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		cleanup()
		_ = body.Close()
		return nil, wrapError(err, "spool maven upstream response")
	}
	_ = body.Close()
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, wrapError(err, "close maven upstream replay body")
	}
	return &replayableBody{path: path}, nil
}

func (b *replayableBody) Open(removeOnClose bool) (io.ReadCloser, error) {
	file, err := os.Open(b.path)
	if err != nil {
		return nil, wrapError(err, "open maven upstream replay body")
	}
	return &replayableReadCloser{File: file, body: b, removeOnClose: removeOnClose}, nil
}

func (b *replayableBody) Remove() {
	if b == nil {
		return
	}
	b.once.Do(func() { _ = os.Remove(b.path) })
}

type replayableReadCloser struct {
	*os.File
	body          *replayableBody
	removeOnClose bool
}

func (r *replayableReadCloser) Close() error {
	err := r.File.Close()
	if r.removeOnClose {
		r.body.Remove()
	}
	return err
}

func shouldServeUpstreamAfterCacheError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return !strings.Contains(message, "validate artifact body") &&
		!strings.Contains(message, "artifact body is empty") &&
		!strings.Contains(message, "invalid artifact content length")
}

func (s *Service) store(ctx context.Context, requestRoute Route, fetched *upstreamFetch) (storedResponse, error) {
	if s.cache == nil {
		return storedResponse{}, oops.In("maven").Errorf("maven cache store is not configured")
	}
	if fetched == nil || fetched.body == nil {
		return storedResponse{}, oops.In("maven").Errorf("maven upstream body is empty")
	}
	defer closeReadCloser(fetched.body, s.logger, "close maven upstream body")
	writeCtx, cancel := context.WithTimeout(ctx, defaultMavenCacheWriteTimeout)
	defer cancel()
	entry, err := s.cache.Put(writeCtx, artifactcache.PutRequest{
		Key:         artifactKey(requestRoute),
		AcceptKey:   acceptKeyMaven,
		Body:        fetched.body,
		Headers:     fetched.headers,
		ContentType: routeContentType(requestRoute, fetched.headers),
		TTL:         routeTTL(requestRoute),
		Validator:   mavenBodyValidator(requestRoute),
	})
	if err != nil {
		return storedResponse{}, wrapError(err, "store maven artifact")
	}
	if entry.Body != nil {
		closeReadCloser(entry.Body, s.logger, "close temporary maven cache body")
	}
	refreshed, ok, err := s.cache.Get(ctx, artifactKey(requestRoute))
	if err != nil {
		return storedResponse{}, wrapError(err, "reopen stored maven artifact")
	}
	if !ok {
		return storedResponse{}, oops.In("maven").Errorf("stored maven artifact is not readable")
	}
	return storedResponse{
		digest:  refreshed.Digest,
		size:    refreshed.Size,
		headers: refreshed.Headers,
		body:    refreshed.Body,
	}, nil
}

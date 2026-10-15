package maven

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"github.com/samber/oops"
)

const defaultMavenCacheWriteTimeout = 30 * time.Second

type replayableBody struct {
	path string
	once sync.Once
}

func newReplayableBody(ctx context.Context, body io.ReadCloser) (*replayableBody, error) {
	if body == nil {
		return nil, oops.In("maven").Errorf("maven upstream body is empty")
	}
	tmp, err := os.CreateTemp("", "regimux-maven-replay-*")
	if err != nil {
		return nil, wrapError(err, "create maven upstream replay body")
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) } //nolint:errcheck // Cleanup is best-effort after the primary failure.
	if _, err := io.Copy(tmp, contextReader{ctx: ctx, reader: body}); err != nil {
		_ = tmp.Close() //nolint:errcheck // Preserve the copy error during cleanup.
		cleanup()
		_ = body.Close() //nolint:errcheck // Preserve the copy error during cleanup.
		return nil, wrapError(err, "spool maven upstream response")
	}
	_ = body.Close() //nolint:errcheck // The body is fully consumed; file close is the relevant error.
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, wrapError(err, "close maven upstream replay body")
	}
	return &replayableBody{path: path}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, wrapError(err, "read maven body")
	}
	n, err := r.reader.Read(p)
	if err != nil && err != io.EOF {
		return n, wrapError(err, "read maven body")
	}
	return n, err //nolint:wrapcheck // io.Reader must preserve the EOF sentinel.
}

func validateMavenReplay(requestRoute Route, fetched *upstreamFetch, replay *replayableBody) error {
	file, err := os.Open(replay.path)
	if err != nil {
		return wrapError(err, "open maven artifact for validation")
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close() //nolint:errcheck // Preserve the stat error during cleanup.
		return wrapError(statErr, "stat maven artifact for validation")
	}
	validationErr := artifactcache.ValidateBody(file, info.Size(), fetched.headers, mavenBodyValidator(requestRoute))
	closeErr := file.Close()
	return errors.Join(validationErr, closeErr)
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
	b.once.Do(func() { _ = os.Remove(b.path) }) //nolint:errcheck // Removal is best-effort cleanup.
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
	return wrapError(err, "close maven replay body")
}

//nolint:cyclop,gocognit,gocyclo // Storage, timeout, reopen, and body ownership are one transaction.
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
		Body:        contextReader{ctx: writeCtx, reader: fetched.body},
		Headers:     fetched.headers,
		ContentType: routeContentType(requestRoute, fetched.headers),
		TTL:         routeTTL(requestRoute),
		Validator:   mavenBodyValidator(requestRoute),
	})
	if err != nil {
		return storedResponse{}, wrapError(err, "store maven artifact")
	}
	s.publishCacheStore(ctx, requestRoute, entry.Digest, entry.Size)
	if entry.Body != nil {
		closeReadCloser(entry.Body, s.logger, "close temporary maven cache body")
	}
	readCtx, stopReadTimeout, readCancel := mavenCacheReadContext(ctx)
	refreshed, ok, err := s.cache.Get(readCtx, artifactKey(requestRoute))
	if err != nil {
		stopReadTimeout()
		readCancel()
		if ctx.Err() != nil {
			return storedResponse{}, wrapError(ctx.Err(), "reopen stored maven artifact")
		}
		return storedResponse{}, wrapError(err, "reopen stored maven artifact")
	}
	if err := ctx.Err(); err != nil {
		stopReadTimeout()
		readCancel()
		closeReadCloser(refreshed.Body, s.logger, "close canceled reopened maven artifact")
		return storedResponse{}, wrapError(err, "reopen stored maven artifact")
	}
	if readErr := readCtx.Err(); readErr != nil {
		stopReadTimeout()
		readCancel()
		closeReadCloser(refreshed.Body, s.logger, "close timed out reopened maven artifact")
		return storedResponse{}, wrapError(readErr, "reopen stored maven artifact")
	}
	if !ok {
		stopReadTimeout()
		readCancel()
		return storedResponse{}, oops.In("maven").Errorf("stored maven artifact is not readable")
	}
	stopReadTimeout()
	if refreshed.Body != nil {
		refreshed.Body = upstreamhttp.WithIdleTimeout(refreshed.Body, readCancel, defaultMavenBodyIdleTimeout)
	} else {
		readCancel()
	}
	return storedResponse{
		digest:  refreshed.Digest,
		size:    refreshed.Size,
		headers: refreshed.Headers,
		body:    refreshed.Body,
	}, nil
}

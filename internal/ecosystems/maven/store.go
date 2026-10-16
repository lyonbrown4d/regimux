package maven

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/samber/oops"
)

const defaultMavenCacheWriteTimeout = 30 * time.Second

type replayableBody struct {
	path            string
	mu              sync.Mutex
	readers         int
	removeRequested bool
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
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.removeRequested {
		return nil, wrapError(os.ErrNotExist, "open released maven replay body")
	}
	file, err := os.Open(b.path)
	if err != nil {
		return nil, wrapError(err, "open maven upstream replay body")
	}
	b.readers++
	return &replayableReadCloser{File: file, body: b, removeOnClose: removeOnClose}, nil
}

func (b *replayableBody) Remove() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeRequested = true
	b.removeIfUnused()
}

// Call with b.mu held. Existing readers retain the file on every supported OS.
func (b *replayableBody) removeIfUnused() {
	if b.removeRequested && b.readers == 0 {
		_ = os.Remove(b.path) //nolint:errcheck // Temporary-file cleanup is best-effort.
	}
}

type replayableReadCloser struct {
	*os.File
	body          *replayableBody
	removeOnClose bool
	closeOnce     sync.Once
	closeErr      error
}

func (r *replayableReadCloser) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.File.Close()
		r.body.mu.Lock()
		defer r.body.mu.Unlock()
		r.body.readers--
		if r.removeOnClose {
			r.body.removeRequested = true
		}
		r.body.removeIfUnused()
	})
	return wrapError(r.closeErr, "close maven replay body")
}

package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
)

const blobOpenFallbackConcurrency int64 = 4

type storedBlobOpenError struct {
	cause error
}

func newStoredBlobOpenError(cause error) error {
	return &storedBlobOpenError{cause: wrapError(cause, "open stored blob object")}
}

func (e *storedBlobOpenError) Error() string { return e.cause.Error() }

func (e *storedBlobOpenError) Unwrap() error { return e.cause }

func (p blobProxy) fallbackAfterStoredObjectOpenError(
	ctx context.Context,
	req BlobRequest,
	result *BlobReadResult,
	cause error,
) (*BlobReadResult, error) {
	if openErr, ok := errors.AsType[*storedBlobOpenError](cause); !ok || openErr == nil || p.openFallbackGate == nil {
		return result, cause
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, wrapError(ctxErr, "fallback after stored blob open")
	}
	release, err := p.acquireOpenFallback(ctx)
	if err != nil {
		return nil, err
	}
	if p.logger != nil {
		p.logger.WarnContext(ctx, "stored blob open failed; fetching upstream",
			"alias", req.UpstreamAlias, "repository", req.Repo, "digest", req.Digest,
			"error_kind", "stored_object_open")
	}
	fallback, err := p.fetchPassthrough(ctx, req)
	if err != nil {
		release()
		return nil, err
	}
	return wrapBlobOpenFallback(ctx, req, fallback, release)
}

func (p blobProxy) acquireOpenFallback(ctx context.Context) (func(), error) {
	if err := p.openFallbackGate.Acquire(ctx, 1); err != nil {
		return nil, wrapError(err, "wait for stored blob fallback")
	}
	release := sync.OnceFunc(func() { p.openFallbackGate.Release(1) })
	if err := ctx.Err(); err != nil {
		release()
		return nil, wrapError(err, "fallback after stored blob open")
	}
	return release, nil
}

func wrapBlobOpenFallback(ctx context.Context, req BlobRequest, fallback *BlobReadResult, release func()) (*BlobReadResult, error) {
	if fallback == nil || fallback.Reader == nil {
		release()
		return nil, errors.New("upstream blob fallback has no body")
	}
	if fallback.Digest != req.Digest {
		cause := distribution.DigestMismatch(req.Digest, fallback.Digest)
		return nil, rejectBlobOpenFallback(fallback.Reader, release, cause)
	}
	verifyBody := (req.Method == "" || req.Method == http.MethodGet) && req.Range == nil && fallback.Status == http.StatusOK
	reader, err := newBlobOpenFallbackReader(ctx, fallback.Reader, fallback.Digest, fallback.Size, verifyBody, release)
	if err != nil {
		return nil, rejectBlobOpenFallback(fallback.Reader, release, err)
	}
	fallback.Reader = reader
	fallback.Cache = CacheBypass
	return fallback, nil
}

func rejectBlobOpenFallback(body io.ReadCloser, release func(), cause error) error {
	closeErr := body.Close()
	release()
	return joinError("validate upstream blob fallback", cause, closeErr)
}

type blobOpenFallbackReader struct {
	body        io.ReadCloser
	verifier    ocidigest.Verifier
	digest      string
	verifyBody  bool
	expected    int64
	read        int64
	release     func()
	releaseOnce sync.Once
	closeOnce   sync.Once
	cancelMu    sync.Mutex
	stopCancel  func() bool
	closeErr    error
	terminal    bool
	terminalErr error
}

func newBlobOpenFallbackReader(
	ctx context.Context,
	body io.ReadCloser,
	digest string,
	size int64,
	verifyBody bool,
	release func(),
) (*blobOpenFallbackReader, error) {
	reader := &blobOpenFallbackReader{body: body, digest: digest, verifyBody: verifyBody, expected: size, release: release}
	if verifyBody {
		parsed, err := ocidigest.Parse(digest)
		if err != nil {
			return nil, fmt.Errorf("parse expected blob digest: %w", err)
		}
		reader.verifier = parsed.Verifier()
		if size == 0 && !reader.verifier.Verified() {
			return nil, distribution.ErrDigestInvalid.WithDetail("upstream blob body does not match " + digest)
		}
	}
	reader.cancelMu.Lock()
	reader.stopCancel = context.AfterFunc(ctx, reader.close)
	reader.cancelMu.Unlock()
	return reader, nil
}

func (r *blobOpenFallbackReader) Read(buffer []byte) (int, error) {
	if r.terminal {
		return 0, r.terminalErr
	}
	n, err := r.body.Read(buffer)
	if progressErr := r.acceptProgress(buffer[:n]); progressErr != nil {
		return n, r.finish(progressErr)
	}
	if r.verifyBody && r.expected >= 0 && r.read == r.expected {
		if verifyErr := r.verifyComplete(); verifyErr != nil {
			return 0, r.finish(verifyErr)
		}
	}
	if errors.Is(err, io.EOF) {
		if verifyErr := r.verifyComplete(); verifyErr != nil {
			return n, r.finish(verifyErr)
		}
		return n, r.finish(io.EOF)
	}
	if err != nil {
		return n, r.finish(err)
	}
	return n, nil
}

func (r *blobOpenFallbackReader) acceptProgress(payload []byte) error {
	if !r.verifyBody || len(payload) == 0 {
		return nil
	}
	next := r.read + int64(len(payload))
	if next < r.read || (r.expected >= 0 && next > r.expected) {
		return errors.New("upstream blob fallback exceeded expected size")
	}
	if _, err := r.verifier.Write(payload); err != nil {
		return fmt.Errorf("hash upstream blob fallback: %w", err)
	}
	r.read = next
	return nil
}

func (r *blobOpenFallbackReader) verifyComplete() error {
	if !r.verifyBody {
		return nil
	}
	if r.expected >= 0 && r.read != r.expected {
		return errors.New("upstream blob fallback size mismatch")
	}
	if !r.verifier.Verified() {
		return distribution.ErrDigestInvalid.WithDetail("upstream blob body does not match " + r.digest)
	}
	return nil
}

func (r *blobOpenFallbackReader) close() {
	// Close retains its error for the response owner while releasing the permit.
	if err := r.Close(); err != nil {
		return
	}
}

func (r *blobOpenFallbackReader) finish(err error) error {
	r.terminal = true
	r.terminalErr = err
	if closeErr := r.Close(); closeErr != nil {
		r.terminalErr = errors.Join(err, closeErr)
	}
	return r.terminalErr
}

func (r *blobOpenFallbackReader) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.body.Close()
		r.releasePermit()
	})
	r.cancelMu.Lock()
	stop := r.stopCancel
	r.stopCancel = nil
	r.cancelMu.Unlock()
	if stop != nil {
		stop()
	}
	return r.closeErr
}

func (r *blobOpenFallbackReader) releasePermit() {
	r.releaseOnce.Do(func() {
		if r.release != nil {
			r.release()
		}
	})
}

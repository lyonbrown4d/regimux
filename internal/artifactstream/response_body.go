package artifactstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

// responseBody observes native transport reads without providing WriterTo,
// which could bypass accounting. Close is safe when a transport closes twice.
type responseBody struct {
	ctx context.Context
	req ResponseRequest

	mu             sync.Mutex
	read           int64
	activeReads    int
	eof            bool
	closeRequested bool
	reported       bool
	startedAt      time.Time

	once     sync.Once
	closeErr error
}

func (b *responseBody) Read(payload []byte) (int, error) {
	b.mu.Lock()
	b.startLocked()
	b.activeReads++
	b.mu.Unlock()

	n, err := b.req.Body.Read(payload)
	b.mu.Lock()
	b.activeReads--
	b.read += int64(n)
	if errors.Is(err, io.EOF) {
		b.eof = true
	}
	diagnostic := b.readDiagnosticLocked(err)
	b.mu.Unlock()
	diagnostic.write(b.ctx, b.req)

	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read artifact response: %w", err)
	}
	return n, nil
}

func (b *responseBody) readDiagnosticLocked(err error) responseDiagnostic {
	short := b.req.Size >= 0 && b.read < b.req.Size
	if diagnostic := b.readErrorDiagnosticLocked(err, short); diagnostic.message != "" {
		return diagnostic
	}
	if errors.Is(err, io.EOF) && short {
		return b.failureLocked("artifact response body length mismatch", "truncated")
	}
	if b.closeRequested && b.activeReads == 0 && short {
		return b.failureLocked("artifact response body closed before expected length", "response_closed_early")
	}
	if b.activeReads == 0 && b.closeErr != nil {
		return b.closeErrorDiagnosticLocked()
	}
	return responseDiagnostic{}
}

func (b *responseBody) readErrorDiagnosticLocked(err error, short bool) responseDiagnostic {
	if err == nil || errors.Is(err, io.EOF) {
		return responseDiagnostic{}
	}
	kind := classifyResponseError(b.ctx, err)
	if kind == "read_error" && b.closeRequested && short {
		return responseDiagnostic{}
	}
	return b.failureLocked("artifact response stream failed", kind)
}

func (b *responseBody) closeErrorDiagnosticLocked() responseDiagnostic {
	kind := classifyResponseError(b.ctx, b.closeErr)
	return b.failureLocked("close artifact response body failed", kind)
}

func (b *responseBody) Close() error {
	b.once.Do(func() {
		b.mu.Lock()
		b.startLocked()
		b.closeRequested = true
		b.mu.Unlock()

		closeErr := b.req.Body.Close()
		b.mu.Lock()
		b.closeErr = closeErr
		diagnostic := b.readDiagnosticLocked(nil)
		b.mu.Unlock()
		diagnostic.write(b.ctx, b.req)
	})
	b.mu.Lock()
	closeErr := b.closeErr
	b.mu.Unlock()
	if closeErr != nil {
		return fmt.Errorf("close artifact response body: %w", closeErr)
	}
	return nil
}

func (b *responseBody) startLocked() {
	if b.startedAt.IsZero() {
		b.startedAt = time.Now()
	}
}

func (b *responseBody) failureLocked(message, kind string) responseDiagnostic {
	if b.reported {
		return responseDiagnostic{}
	}
	b.reported = true
	b.startLocked()
	return responseDiagnostic{
		message: message,
		kind:    classifyResponseError(b.ctx, nil, kind),
		read:    b.read,
		size:    b.req.Size,
		elapsed: time.Since(b.startedAt).Milliseconds(),
	}
}

type responseDiagnostic struct {
	message string
	kind    string
	read    int64
	size    int64
	elapsed int64
}

func (d responseDiagnostic) write(ctx context.Context, req ResponseRequest) {
	if d.message == "" {
		return
	}
	logger := req.Logger
	if logger == nil {
		logger = slog.Default()
	}
	var deadline time.Time
	hasDeadline := false
	var contextErr error
	if ctx != nil {
		deadline, hasDeadline = ctx.Deadline()
		contextErr = ctx.Err()
	}
	attrs := []any{
		"ecosystem", req.Ecosystem,
		"alias", req.Alias,
		"cache", req.Cache,
		"error_kind", d.kind,
		"bytes_read", d.read,
		"expected_bytes", d.size,
		"elapsed_ms", d.elapsed,
		"caller_deadline_set", hasDeadline,
		"caller_canceled", errors.Is(contextErr, context.Canceled),
		"caller_deadline_exceeded", errors.Is(contextErr, context.DeadlineExceeded),
	}
	if hasDeadline {
		attrs = append(attrs, "caller_deadline_remaining_ms", time.Until(deadline).Milliseconds())
	}
	if ctx == nil {
		logger.Error(d.message, attrs...)
		return
	}
	logger.ErrorContext(ctx, d.message, attrs...)
}

func classifyResponseError(ctx context.Context, err error, fallback ...string) string {
	if ctx != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			return "caller_deadline"
		case context.Canceled:
			return "caller_canceled"
		}
	}
	return classifySourceError(err, fallback)
}

func classifySourceError(err error, fallback []string) string {
	if errors.Is(err, transfer.ErrContinuationFailed) {
		return "continuation_failed"
	}
	if errors.Is(err, transfer.ErrObjectOpenTimeout) {
		return "object_open_timeout"
	}
	if errors.Is(err, upstreamhttp.ErrBodyIdleTimeout) {
		return "body_idle_timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "source_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "source_deadline"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "truncated"
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return "read_error"
}

func (b *responseBody) close() {
	// Close reports its own failure with the response identity.
	if err := b.Close(); err != nil {
		return
	}
}

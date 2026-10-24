// Package transfer provides bounded, streaming artifact range and recovery helpers.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/sethvargo/go-retry"
)

// Open reopens an immutable representation after consumed bytes, relative to
// the original response. It must validate identity and the requested offset.
type Open func(context.Context, int64) (io.ReadCloser, error)

// Recover retries at most three interrupted reads without replaying bytes.
// A canceled caller never retries. Close may interrupt a blocked Read.
func Recover(ctx context.Context, body io.ReadCloser, size int64, open Open) io.ReadCloser {
	if size < 0 || open == nil || body == nil {
		return body
	}
	readCtx, cancel := context.WithCancel(ctx)
	return &recoveryBody{ctx: readCtx, cancel: cancel, body: &onceBody{ReadCloser: body}, size: size, open: open}
}

type recoveryBody struct {
	ctx      context.Context
	cancel   context.CancelFunc
	body     io.ReadCloser
	size     int64
	open     Open
	read     int64
	retries  uint64
	pending  error
	terminal error
	mu       sync.Mutex
	closed   bool
}

func (b *recoveryBody) Read(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	for {
		body, err := b.ready()
		if err != nil {
			b.terminal = err
			return 0, err
		}
		n, err := b.readBody(body, payload)
		if b.shouldRecover(err) {
			b.pending = err
			if n > 0 {
				return n, nil
			}
			continue
		}
		n, err = readResult(n, err)
		b.terminal = b.finalReadError(err)
		return n, b.terminal
	}
}

func (b *recoveryBody) finalReadError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || b.retries == 0 {
		return err
	}
	return b.continuationError(err)
}

func (b *recoveryBody) continuationError(err error) error {
	if b.ctx.Err() != nil {
		return err
	}
	return errors.Join(ErrContinuationFailed, err)
}

func (b *recoveryBody) ready() (io.ReadCloser, error) {
	if b.terminal != nil {
		return nil, b.terminal
	}
	if err := b.ctx.Err(); err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	if b.pending != nil {
		if err := b.reopen(); err != nil {
			return nil, err
		}
	}
	body, closed := b.current()
	if closed {
		return nil, io.ErrClosedPipe
	}
	return body, nil
}

func (b *recoveryBody) readBody(body io.Reader, payload []byte) (int, error) {
	n, err := body.Read(payload)
	if n < 0 || n > len(payload) {
		return 0, errors.New("artifact body returned an invalid byte count")
	}
	remaining := b.size - b.read
	if int64(n) > remaining {
		b.read += remaining
		return len(payload[:remaining]), errors.New("artifact body exceeds declared length")
	}
	b.read += int64(n)
	if err == io.EOF && b.read < b.size {
		return n, io.ErrUnexpectedEOF
	}
	return readResult(n, err)
}

func (b *recoveryBody) shouldRecover(err error) bool {
	return b.read < b.size && b.retries < 3 && recoverable(err)
}

func readResult(n int, err error) (int, error) {
	if err == nil {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, fmt.Errorf("read artifact transfer: %w", err)
}

func (b *recoveryBody) current() (io.ReadCloser, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body, b.closed
}

func (b *recoveryBody) reopen() error {
	if b.retries >= 3 {
		return fmt.Errorf("artifact continuation budget exhausted: %w", b.continuationError(b.pending))
	}
	previous, closed := b.current()
	if closed {
		return io.ErrClosedPipe
	}
	if err := previous.Close(); err != nil && b.ctx.Err() != nil {
		return fmt.Errorf("close interrupted artifact: %w", err)
	}
	backoff := retry.WithMaxRetries(2-b.retries,
		retry.WithJitterPercent(20, retry.NewExponential(50*time.Millisecond)))
	next, err := retry.DoValue(b.ctx, backoff, b.openNext)
	if err != nil {
		return fmt.Errorf("resume artifact at %d: %w", b.read, b.continuationError(err))
	}
	if next == nil {
		return errors.New("resume artifact returned an empty reader")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errors.Join(io.ErrClosedPipe, next.Close())
	}
	b.body = &onceBody{ReadCloser: next}
	b.pending = nil
	return nil
}

func (b *recoveryBody) openNext(ctx context.Context) (io.ReadCloser, error) {
	b.retries++
	next, err := b.open(ctx, b.read)
	if err == nil {
		return next, nil
	}
	if next != nil {
		err = errors.Join(err, next.Close())
	}
	if recoverable(err) && b.retries < 3 {
		return nil, fmt.Errorf("retry artifact continuation: %w", retry.RetryableError(err))
	}
	return nil, fmt.Errorf("open artifact continuation: %w", err)
}

func (b *recoveryBody) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	body := b.body
	b.mu.Unlock()
	b.cancel()
	if err := body.Close(); err != nil {
		return fmt.Errorf("close artifact recovery body: %w", err)
	}
	return nil
}

func recoverable(err error) bool {
	var networkError net.Error
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, io.ErrClosedPipe) || (errors.As(err, &networkError) && networkError.Timeout())
}

type onceBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

// CancelOnClose owns an outer transfer context without imposing another idle
// timer over per-attempt timers. A recovering read can span several attempts.
func CancelOnClose(body io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	return &cancelBody{ReadCloser: body, cancel: cancel}
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
	err    error
}

func (b *cancelBody) Close() error {
	b.once.Do(func() {
		b.cancel()
		b.err = b.ReadCloser.Close()
	})
	return b.err
}

func (b *onceBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

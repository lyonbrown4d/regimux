package upstreamhttp

import (
	"context"
	"io"
	"sync"
	"time"
)

// DefaultBodyIdleTimeout bounds inactivity before an interrupted attempt is
// resumed, leaving time within common package-manager download deadlines.
const DefaultBodyIdleTimeout = 10 * time.Second

// WithIdleTimeout starts the idle budget at the first nonempty Read, then
// cancels the response context if reads stop making progress for timeout.
// The owner must Close even an unread body to release its request resources.
func WithIdleTimeout(body io.ReadCloser, cancel context.CancelFunc, timeout time.Duration) io.ReadCloser {
	if timeout <= 0 {
		timeout = DefaultBodyIdleTimeout
	}
	return &idleResponseBody{ReadCloser: body, cancel: cancel, timeout: timeout}
}

type idleResponseBody struct {
	io.ReadCloser
	cancel   context.CancelFunc
	timeout  time.Duration
	timer    *time.Timer
	mu       sync.Mutex
	deadline time.Time
	closed   bool
	timedOut bool
	once     sync.Once
	err      error
}

func (b *idleResponseBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !b.beginRead() {
		return 0, io.ErrClosedPipe
	}
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.mu.Lock()
		b.stopTimer()
		b.mu.Unlock()
	}
	return n, err //nolint:wrapcheck // io.Reader must preserve the EOF sentinel.
}

func (b *idleResponseBody) beginRead() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	if !b.timedOut {
		b.deadline = time.Now().Add(b.timeout)
		if b.timer == nil {
			b.timer = time.AfterFunc(b.timeout, b.expire)
		} else {
			b.timer.Reset(b.timeout)
		}
	}
	return true
}

func (b *idleResponseBody) expire() {
	b.mu.Lock()
	// Reset cannot retract a callback already queued by an older deadline.
	if b.closed || b.timedOut || b.deadline.IsZero() || time.Now().Before(b.deadline) {
		b.mu.Unlock()
		return
	}
	b.timedOut = true
	b.mu.Unlock()
	b.cancel()
}

// stopTimer requires b.mu. Clearing the deadline also invalidates queued callbacks.
func (b *idleResponseBody) stopTimer() {
	b.deadline = time.Time{}
	if b.timer != nil {
		b.timer.Stop()
	}
}

func (b *idleResponseBody) Close() error {
	b.once.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.stopTimer()
		b.mu.Unlock()
		b.cancel()
		b.err = b.ReadCloser.Close()
	})
	return b.err
}

package upstreamhttp

import (
	"context"
	"io"
	"sync"
	"time"
)

// WithIdleTimeout keeps a response context alive while its body is consumed,
// then cancels it if reads stop making progress for timeout. Close always
// cancels the request context and closes the response body.
func WithIdleTimeout(body io.ReadCloser, cancel context.CancelFunc, timeout time.Duration) io.ReadCloser {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &idleResponseBody{ReadCloser: body, cancel: cancel, timeout: timeout, timer: time.AfterFunc(timeout, cancel)}
}

type idleResponseBody struct {
	io.ReadCloser
	cancel  context.CancelFunc
	timeout time.Duration
	timer   *time.Timer
	once    sync.Once
	err     error
}

func (b *idleResponseBody) Read(p []byte) (int, error) {
	b.timer.Reset(b.timeout)
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.timer.Stop()
	}
	return n, err //nolint:wrapcheck // io.Reader must preserve the EOF sentinel.
}

func (b *idleResponseBody) Close() error {
	b.once.Do(func() {
		b.timer.Stop()
		b.cancel()
		b.err = b.ReadCloser.Close()
	})
	return b.err
}

package upstreamhttp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

func TestWithIdleTimeoutCancelsStalledBodyRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := upstreamhttp.WithIdleTimeout(contextBody{ctx: ctx}, cancel, 20*time.Millisecond)
	t.Cleanup(func() {
		if err := body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	})

	_, err := body.Read(make([]byte, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want context.Canceled", err)
	}
}

func TestWithIdleTimeoutCloseCancelsRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := upstreamhttp.WithIdleTimeout(io.NopCloser(&immediateReader{}), cancel, time.Hour)
	if err := body.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context error = %v, want context.Canceled", ctx.Err())
	}
}

type contextBody struct{ ctx context.Context }

func (b contextBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, fmt.Errorf("read body: %w", b.ctx.Err())
}

func (contextBody) Close() error { return nil }

type immediateReader struct{}

func (*immediateReader) Read([]byte) (int, error) { return 0, io.EOF }

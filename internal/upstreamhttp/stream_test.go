package upstreamhttp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"github.com/stretchr/testify/require"
)

func TestWithIdleTimeoutStartsWhenBodyConsumptionBegins(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		body := upstreamhttp.WithIdleTimeout(contextBody{ctx: ctx}, cancel, 20*time.Millisecond)
		t.Cleanup(func() {
			if err := body.Close(); err != nil {
				t.Errorf("close body: %v", err)
			}
		})

		time.Sleep(30 * time.Millisecond)
		if err := ctx.Err(); err != nil {
			t.Fatalf("unused response canceled before first read: %v", err)
		}
		started := time.Now()
		_, err := body.Read(make([]byte, 1))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Read() error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(started); elapsed != 20*time.Millisecond {
			t.Fatalf("stalled read canceled after %s, want 20ms", elapsed)
		}
	})
}

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

func TestWithIdleTimeoutBoundsPausesBetweenReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		reader := &progressThenStallBody{ctx: ctx}
		body := upstreamhttp.WithIdleTimeout(reader, cancel, 20*time.Millisecond)
		t.Cleanup(func() {
			require.NoError(t, body.Close(), "close body")
		})
		for range 3 {
			n, err := body.Read(make([]byte, 1))
			require.Equal(t, 1, n, "progress read byte count")
			require.NoError(t, err, "progress read")
			time.Sleep(15 * time.Millisecond)
			require.NoError(t, ctx.Err(), "progressing response canceled")
		}
		time.Sleep(10 * time.Millisecond)
		require.ErrorIs(t, ctx.Err(), context.Canceled, "idle response context")
	})
}

func TestWithIdleTimeoutConcurrentFirstReadAndClose(t *testing.T) {
	for range 50 {
		ctx, cancel := context.WithCancel(t.Context())
		body := upstreamhttp.WithIdleTimeout(contextBody{ctx: ctx}, cancel, time.Hour)
		done := make(chan error, 1)
		go func() {
			_, err := body.Read(make([]byte, 1))
			done <- err
		}()
		if err := body.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("Read() after concurrent close = %v", err)
		}
	}
}

type progressThenStallBody struct {
	ctx context.Context
}

func (b *progressThenStallBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read progressing body: %w", err)
	}
	p[0] = 'x'
	return 1, nil
}

func (*progressThenStallBody) Close() error { return nil }

type contextBody struct{ ctx context.Context }

func (b contextBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, fmt.Errorf("read body: %w", b.ctx.Err())
}

func (contextBody) Close() error { return nil }

type immediateReader struct{}

func (*immediateReader) Read([]byte) (int, error) { return 0, io.EOF }

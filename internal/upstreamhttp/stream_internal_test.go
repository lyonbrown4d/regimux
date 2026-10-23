package upstreamhttp

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdleResponseBodyIgnoresQueuedExpiryAfterProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		wrapped := WithIdleTimeout(io.NopCloser(strings.NewReader("abcdef")), cancel, 20*time.Millisecond)
		body, ok := wrapped.(*idleResponseBody)
		require.True(t, ok, "idle response wrapper has expected type")
		t.Cleanup(func() {
			closeErr := body.Close()
			require.NoError(t, closeErr)
		})

		buffer := make([]byte, 1)
		n, err := body.Read(buffer)
		require.NoError(t, err)
		require.Equal(t, 1, n, "first read byte count")
		time.Sleep(15 * time.Millisecond)
		n, err = body.Read(buffer)
		require.NoError(t, err)
		require.Equal(t, 1, n, "progress read byte count")

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		body.expire() // Simulate an older timer callback queued before the read reset.
		require.NoError(t, ctx.Err(), "stale expiry must not cancel a progressing response")

		time.Sleep(10 * time.Millisecond)
		synctest.Wait()
		require.ErrorIs(t, ctx.Err(), context.Canceled, "current idle deadline")
	})
}

func TestIdleResponseBodyQueuedExpiryAfterEOFWaitsForClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		wrapped := WithIdleTimeout(io.NopCloser(strings.NewReader("complete")), cancel, 20*time.Millisecond)
		body, ok := wrapped.(*idleResponseBody)
		require.True(t, ok, "idle response wrapper has expected type")

		got, err := io.ReadAll(body)
		require.NoError(t, err, "read body through EOF")
		require.Equal(t, "complete", string(got))
		time.Sleep(25 * time.Millisecond)
		synctest.Wait()
		body.expire() // Simulate a callback that was already queued when EOF stopped the timer.
		require.NoError(t, ctx.Err(), "expiry after EOF must wait for Close")
		require.NoError(t, body.Close(), "close completed body")
		require.ErrorIs(t, ctx.Err(), context.Canceled, "Close cancels the response context")
	})
}

package upstreamhttp_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"github.com/stretchr/testify/require"
)

func TestWithIdleTimeoutResetsBudgetAfterReadProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		body := upstreamhttp.WithIdleTimeout(&pacedProgressBody{ctx: ctx}, cancel, 20*time.Millisecond)
		t.Cleanup(func() { require.NoError(t, body.Close()) })
		for range 3 {
			n, err := body.Read(make([]byte, 1))
			require.NoError(t, err, "a read completing within its budget must succeed")
			require.Equal(t, 1, n)
			time.Sleep(15 * time.Millisecond)
			require.NoError(t, ctx.Err(), "successful progress must renew the consumer's idle budget")
		}
		time.Sleep(6 * time.Millisecond)
		synctest.Wait()
		require.ErrorIs(t, ctx.Err(), context.Canceled, "a full idle budget after the last progress remains bounded")
	})
}

func TestWithIdleTimeoutIdentifiesIdleBudgetExpiration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		body := upstreamhttp.WithIdleTimeout(contextBody{ctx: ctx}, cancel, 20*time.Millisecond)
		t.Cleanup(func() { require.NoError(t, body.Close()) })
		_, err := body.Read(make([]byte, 1))
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "body idle timeout")
		require.ErrorIs(t, err, upstreamhttp.ErrBodyIdleTimeout)
	})
}

func TestWithIdleTimeoutDoesNotRenewWithoutReadProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		body := upstreamhttp.WithIdleTimeout(zeroProgressBody{ctx: ctx}, cancel, 20*time.Millisecond)
		t.Cleanup(func() { require.NoError(t, body.Close()) })
		n, err := body.Read(make([]byte, 1))
		require.Zero(t, n)
		require.NoError(t, err)
		_, err = body.Read(make([]byte, 1))
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, err, upstreamhttp.ErrBodyIdleTimeout)
	})
}

func TestWithIdleTimeoutStopsZeroReadsThatIgnoreCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		body := upstreamhttp.WithIdleTimeout(zeroProgressBody{ctx: context.WithoutCancel(ctx)}, cancel, 20*time.Millisecond)
		t.Cleanup(func() { require.NoError(t, body.Close()) })
		_, err := body.Read(make([]byte, 1))
		require.NoError(t, err)
		_, err = body.Read(make([]byte, 1))
		require.ErrorIs(t, err, upstreamhttp.ErrBodyIdleTimeout)
	})
}

type zeroProgressBody struct{ ctx context.Context }

func (b zeroProgressBody) Read([]byte) (int, error) {
	time.Sleep(15 * time.Millisecond)
	if err := b.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read empty response: %w", err)
	}
	return 0, nil
}

func (zeroProgressBody) Close() error { return nil }

type pacedProgressBody struct{ ctx context.Context }

func (b *pacedProgressBody) Read(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	time.Sleep(15 * time.Millisecond)
	if err := b.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read paced response: %w", err)
	}
	payload[0] = 'x'
	return 1, nil
}

func (*pacedProgressBody) Close() error { return nil }

var _ io.ReadCloser = (*pacedProgressBody)(nil)

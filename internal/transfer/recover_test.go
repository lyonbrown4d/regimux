package transfer_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoveryPreservesBytesOffsetsAndCloseOwnership(t *testing.T) {
	first := &countedBody{Reader: strings.NewReader("abc")}
	second := &countedBody{Reader: strings.NewReader("de")}
	third := &countedBody{Reader: strings.NewReader("fgh")}
	offsets := []int64{}
	body := transfer.Recover(t.Context(), first, 8, func(_ context.Context, offset int64) (io.ReadCloser, error) {
		offsets = append(offsets, offset)
		if offset == 3 {
			return second, nil
		}
		if offset == 5 {
			return third, nil
		}
		return nil, fmt.Errorf("unexpected offset %d", offset)
	})
	actual, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "abcdefgh", string(actual))
	require.NoError(t, body.Close())
	require.NoError(t, body.Close())
	require.Equal(t, []int64{3, 5}, offsets)
	for _, source := range []*countedBody{first, second, third} {
		require.Equal(t, int64(1), source.closes.Load())
	}
}

func TestRecoveryStopsAfterThreeContinuations(t *testing.T) {
	var opens int
	body := transfer.Recover(t.Context(), io.NopCloser(strings.NewReader("a")), 10,
		func(context.Context, int64) (io.ReadCloser, error) {
			opens++
			return io.NopCloser(strings.NewReader("")), nil
		})
	actual, err := io.ReadAll(body)
	if !errors.Is(err, io.ErrUnexpectedEOF) || string(actual) != "a" || opens != 3 {
		t.Fatalf("body=%q error=%v opens=%d", actual, err, opens)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryTerminalFailureRemainsStickyAcrossReads(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		cause error
		limit int
	}{
		{name: "continuation budget", cause: io.ErrUnexpectedEOF, limit: 3},
		{name: "permanent identity error", cause: errors.New("representation identity changed"), limit: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var opens int
			body := transfer.Recover(t.Context(), io.NopCloser(strings.NewReader("a")), 4,
				func(context.Context, int64) (io.ReadCloser, error) {
					opens++
					if opens > testCase.limit {
						return io.NopCloser(strings.NewReader("bcd")), nil
					}
					return nil, testCase.cause
				})
			t.Cleanup(func() { require.NoError(t, body.Close()) })
			actual, firstErr := io.ReadAll(body)
			require.Equal(t, "a", string(actual))
			require.ErrorIs(t, firstErr, testCase.cause)
			for range 3 {
				n, nextErr := body.Read(make([]byte, 8))
				assert.Zero(t, n, "read after terminal failure returned more bytes")
				assert.ErrorIs(t, nextErr, testCase.cause, "terminal failure changed across reads")
			}
			require.Equal(t, testCase.limit, opens, "terminal error restarted the transfer")
		})
	}
}

func TestRecoveryClipsSourceOverrunToDeclaredLength(t *testing.T) {
	body := transfer.Recover(t.Context(), io.NopCloser(strings.NewReader("abcdef")), 3,
		func(context.Context, int64) (io.ReadCloser, error) {
			t.Error("length overrun must not recover")
			return nil, errors.New("unexpected continuation")
		})
	t.Cleanup(func() { require.NoError(t, body.Close()) })
	actual, err := io.ReadAll(body)
	require.Error(t, err)
	require.Equal(t, "abc", string(actual), "bytes beyond the declared length escaped to the consumer")
	n, repeatedErr := body.Read(make([]byte, 8))
	require.Zero(t, n)
	require.Error(t, repeatedErr)
}

func TestRecoveryHonorsCancellationBeforeOpening(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	body := transfer.Recover(ctx, io.NopCloser(strings.NewReader("abc")), 8,
		func(context.Context, int64) (io.ReadCloser, error) {
			t.Error("canceled transfer reopened its source")
			return httpEmptyBody(), nil
		})
	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryCloseInterruptsOpening(t *testing.T) {
	entered := make(chan struct{})
	body := transfer.Recover(t.Context(), httpEmptyBody(), 8,
		func(ctx context.Context, _ int64) (io.ReadCloser, error) {
			close(entered)
			<-ctx.Done()
			return nil, fmt.Errorf("open canceled: %w", ctx.Err())
		})
	finished := make(chan error, 1)
	go func() { _, err := io.ReadAll(body); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("continuation did not start")
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel continuation")
	}
}

func TestRecoveryDoesNotRetryPermanentReadErrors(t *testing.T) {
	want := errors.New("invalid artifact framing")
	body := transfer.Recover(t.Context(), &errorBody{err: want}, 8,
		func(context.Context, int64) (io.ReadCloser, error) {
			t.Error("permanent read error retried")
			return httpEmptyBody(), nil
		})
	if _, err := io.ReadAll(body); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
}

type countedBody struct {
	io.Reader
	closes atomic.Int64
}

func (b *countedBody) Close() error { b.closes.Add(1); return nil }

type errorBody struct{ err error }

func (b *errorBody) Read([]byte) (int, error) { return 0, b.err }
func (*errorBody) Close() error               { return nil }

func httpEmptyBody() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }

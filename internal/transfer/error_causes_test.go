package transfer_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestOpenObjectIdentifiesInternalOpenTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := ocidigest.SHA256.FromString("abc").String()
		reader, _, err := transfer.OpenObject(t.Context(), &blockedOpenCauseStore{}, transfer.ObjectRequest{
			Key: key, Digest: key, Size: 3, OpenTimeout: 20 * time.Millisecond,
		})
		require.Nil(t, reader)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, "artifact object open timeout")
		require.ErrorIs(t, err, transfer.ErrObjectOpenTimeout)
		require.NoError(t, t.Context().Err(), "the caller itself has not timed out")
	})
}

func TestRecoverIdentifiesFailedContinuationOpen(t *testing.T) {
	cause := errors.New("synthetic origin unavailable")
	body := transfer.Recover(t.Context(), io.NopCloser(strings.NewReader("a")), 2,
		func(context.Context, int64) (io.ReadCloser, error) { return nil, cause })
	t.Cleanup(func() { require.NoError(t, body.Close()) })
	data, err := io.ReadAll(body)
	require.Equal(t, "a", string(data))
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "artifact continuation failed")
	require.ErrorIs(t, err, transfer.ErrContinuationFailed)
}

func TestRecoverIdentifiesFailedContinuationBody(t *testing.T) {
	cause := errors.New("synthetic body failure")
	body := transfer.Recover(t.Context(), io.NopCloser(strings.NewReader("a")), 2,
		func(context.Context, int64) (io.ReadCloser, error) { return &continuationCauseBody{cause: cause}, nil })
	t.Cleanup(func() { require.NoError(t, body.Close()) })
	data, err := io.ReadAll(body)
	require.Equal(t, "a", string(data))
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "artifact continuation failed")
	require.ErrorIs(t, err, transfer.ErrContinuationFailed)
}

type blockedOpenCauseStore struct{ object.Store }

func (*blockedOpenCauseStore) Get(ctx context.Context, _ string, _ object.GetOptions) (io.ReadCloser, *object.Info, error) {
	<-ctx.Done()
	return nil, nil, fmt.Errorf("wait for synthetic object: %w", ctx.Err())
}

type continuationCauseBody struct{ cause error }

func (b *continuationCauseBody) Read([]byte) (int, error) {
	return 0, fmt.Errorf("read synthetic continuation: %w", b.cause)
}

func (*continuationCauseBody) Close() error { return nil }

package upstreamhttp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"github.com/stretchr/testify/require"
)

func TestOpenStreamHeaderTimeoutClosesLateBody(t *testing.T) {
	parent := t.Context()
	body := &streamTestBody{}
	var attemptErr func() error
	response, err := upstreamhttp.OpenStream(parent, 20*time.Millisecond, time.Second,
		func(ctx context.Context) (*upstreamhttp.Response, error) {
			attemptErr = ctx.Err
			<-ctx.Done()
			return &upstreamhttp.Response{Body: body}, nil
		})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, response)
	require.Equal(t, int32(1), body.closes.Load())
	require.ErrorIs(t, attemptErr(), context.Canceled)
	require.NoError(t, parent.Err())
}

func TestOpenStreamKeepsProgressingBodyPastHeaderTimeout(t *testing.T) {
	parent := t.Context()
	var body *streamTestBody
	response, err := upstreamhttp.OpenStream(parent, 20*time.Millisecond, 200*time.Millisecond,
		func(ctx context.Context) (*upstreamhttp.Response, error) {
			body = &streamTestBody{ctx: ctx, content: "body", delay: 40 * time.Millisecond}
			return &upstreamhttp.Response{Body: body}, nil
		})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
	actual, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "body", string(actual))
	require.NoError(t, body.ctx.Err(), "header timeout canceled a progressing body")
	require.NoError(t, response.Body.Close())
	require.ErrorIs(t, body.ctx.Err(), context.Canceled)
	require.NoError(t, parent.Err())
	require.Equal(t, int32(1), body.closes.Load())
}

func TestOpenStreamRejectsIncompleteAndFailedResponses(t *testing.T) {
	want := errors.New("upstream transport failure")
	for _, testCase := range []struct {
		name     string
		response *upstreamhttp.Response
		err      error
	}{
		{name: "missing response"},
		{name: "missing body", response: &upstreamhttp.Response{}},
		{name: "transport error with body", response: &upstreamhttp.Response{Body: &streamTestBody{}}, err: want},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var attemptErr func() error
			response, err := upstreamhttp.OpenStream(t.Context(), time.Second, time.Second,
				func(ctx context.Context) (*upstreamhttp.Response, error) {
					attemptErr = ctx.Err
					return testCase.response, testCase.err
				})
			require.Error(t, err)
			require.Nil(t, response)
			require.ErrorIs(t, attemptErr(), context.Canceled)
			require.NoError(t, t.Context().Err())
			if testCase.err != nil {
				require.ErrorIs(t, err, want)
				body, ok := testCase.response.Body.(*streamTestBody)
				require.True(t, ok)
				require.Equal(t, int32(1), body.closes.Load())
			}
		})
	}
}

func TestOpenStreamIdleTimeoutOnlyCancelsAttempt(t *testing.T) {
	response, err := upstreamhttp.OpenStream(t.Context(), time.Second, 20*time.Millisecond,
		func(ctx context.Context) (*upstreamhttp.Response, error) {
			return &upstreamhttp.Response{Body: contextBody{ctx: ctx}}, nil
		})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
	_, err = response.Body.Read(make([]byte, 1))
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, t.Context().Err())
}

type streamTestBody struct {
	ctx     context.Context
	content string
	delay   time.Duration
	closes  atomic.Int32
}

func (b *streamTestBody) Read(payload []byte) (int, error) {
	if b.content == "" {
		return 0, io.EOF
	}
	timer := time.NewTimer(b.delay)
	defer timer.Stop()
	select {
	case <-b.ctx.Done():
		return 0, fmt.Errorf("read stream test body: %w", b.ctx.Err())
	case <-timer.C:
	}
	if len(payload) == 0 {
		return 0, nil
	}
	payload[0] = b.content[0]
	b.content = b.content[1:]
	return 1, nil
}

func (b *streamTestBody) Close() error {
	b.closes.Add(1)
	return nil
}

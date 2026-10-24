package object

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiskCacheCloseCancelsBlockedBackendGet(t *testing.T) {
	for _, testCase := range []struct {
		name string
		opts GetOptions
	}{
		{name: "full"},
		{name: "range", opts: GetOptions{Range: &HTTPRange{Start: 1, End: 3}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertDiskCacheCloseCancelsBlockedGet(t, testCase.opts)
		})
	}
}

func assertDiskCacheCloseCancelsBlockedGet(t *testing.T, opts GetOptions) {
	t.Helper()
	backend, digest, payload := newDiskCacheTestBackend(t)
	info, err := backend.canonical.Stat(t.Context(), digest)
	require.NoError(t, err, "get canonical metadata")
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	source := &diskCacheCloseTrackingReader{Reader: bytes.NewReader(payload), closed: make(chan struct{})}
	backend.get = func(ctx context.Context, _ string, _ GetOptions) (io.ReadCloser, *Info, error) {
		context.AfterFunc(ctx, func() { close(canceled) })
		close(started)
		<-release
		return source, info, nil
	}
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	type getResult struct {
		reader io.ReadCloser
		info   *Info
		err    error
	}
	resultChannel := make(chan getResult, 1)
	go func() {
		reader, resultInfo, getErr := cache.Get(t.Context(), digest, opts)
		resultChannel <- getResult{reader: reader, info: resultInfo, err: getErr}
	}()
	waitForDiskCacheSignal(t, started)
	closeDone := make(chan error, 1)
	go func() { closeDone <- cache.Close() }()
	waitForDiskCacheSignal(t, canceled)
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-closeDone, "close disk cache")
	result := <-resultChannel
	require.Error(t, result.err, "Get must fail after cache Close")
	require.True(t, errors.Is(result.err, errDiskCacheClosed) || errors.Is(result.err, context.Canceled), "Get error = %v, want closed or canceled", result.err)
	require.Nil(t, result.reader, "late backend reader must not be delivered")
	require.Nil(t, result.info, "late backend metadata must not be delivered")
	waitForDiskCacheSignal(t, source.closed)
	require.Equal(t, 0, cache.Stats().Fills, "Close leaves no active fills")
}

type diskCacheCloseTrackingReader struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (r *diskCacheCloseTrackingReader) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func TestDiskCachePublishesAfterExactSizeCopyNAndClose(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	reader, info, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "open canonical object")
	require.EqualValues(t, len(payload), info.Size, "canonical object size")

	var copied bytes.Buffer
	count, err := io.CopyN(&copied, reader, info.Size)
	require.NoError(t, err, "copy exactly the advertised object size")
	require.Equal(t, info.Size, count, "bytes copied")
	require.Equal(t, payload, copied.Bytes(), "copied payload")
	require.NoError(t, reader.Close(), "close after exact-size copy without reading EOF")
	require.Equal(t, 1, cache.Stats().Entries, "exact-size close publishes a complete object")

	readCachedObject(t, cache, digest, payload)
	require.EqualValues(t, 1, backend.gets.Load(), "second Get is served from warm cache")
}

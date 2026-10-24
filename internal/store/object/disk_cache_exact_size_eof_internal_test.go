package object

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type diskCacheExactSizeReader struct {
	data      *bytes.Reader
	sizeRead  chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	sizeOnce  sync.Once
}

func TestDiskCacheKnownSizeDoesNotWaitForSourceEOF(t *testing.T) {
	for _, payload := range [][]byte{[]byte("known-size-body"), {}} {
		t.Run(testDiskCacheSizeName(len(payload)), func(t *testing.T) {
			assertDiskCacheKnownSizeCompletesWithoutSourceEOF(t, payload)
		})
	}
}

func assertDiskCacheKnownSizeCompletesWithoutSourceEOF(t *testing.T, payload []byte) {
	t.Helper()
	backend, _, _ := newDiskCacheTestBackend(t)
	digest := digestForTest(payload)
	source := newDiskCacheExactSizeReader(payload)
	info := &Info{Digest: digest, Size: int64(len(payload)), ContentType: "application/octet-stream"}
	backend.get = func(context.Context, string, GetOptions) (io.ReadCloser, *Info, error) {
		return source, info, nil
	}
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	reader, _, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "open known-size object")
	if len(payload) > 0 {
		waitForDiskCacheSignal(t, source.sizeRead)
	}

	result := make(chan error, 1)
	go func() {
		if len(payload) == 0 {
			_, readErr := io.Copy(io.Discard, reader)
			result <- readErr
			return
		}
		_, readErr := io.CopyN(io.Discard, reader, int64(len(payload)))
		if readErr == nil {
			readErr = reader.Close()
		}
		result <- readErr
	}()
	select {
	case readErr := <-result:
		require.NoError(t, readErr, "read and close exact-size body")
	case <-time.After(500 * time.Millisecond):
		t.Error("exact-size consumer waited for an extra source EOF read")
		stack := make([]byte, 64*1024)
		stackLen := runtime.Stack(stack, true)
		t.Logf("goroutine stack before releasing source:\n%s", stack[:stackLen])
		require.NoError(t, source.Close(), "release blocked source during cleanup")
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Error("exact-size consumer did not stop after source close")
		}
		require.NoError(t, cache.Close(), "close cache after releasing source")
		return
	}
	require.Equal(t, 1, cache.Stats().Entries, "complete known-size body is cached")
}

func newDiskCacheExactSizeReader(payload []byte) *diskCacheExactSizeReader {
	return &diskCacheExactSizeReader{
		data:     bytes.NewReader(payload),
		sizeRead: make(chan struct{}),
		closed:   make(chan struct{}),
	}
}

func (r *diskCacheExactSizeReader) Read(buffer []byte) (int, error) {
	if r.data.Len() > 0 {
		n, err := r.data.Read(buffer)
		if r.data.Len() == 0 {
			r.sizeOnce.Do(func() { close(r.sizeRead) })
		}
		if err == io.EOF {
			return n, io.EOF
		}
		if err != nil {
			return n, fmt.Errorf("read exact-size test source: %w", err)
		}
		return n, nil
	}
	<-r.closed
	return 0, io.EOF
}

func (r *diskCacheExactSizeReader) Close() error {
	r.closeOnce.Do(func() { close(r.closed) })
	return nil
}

func testDiskCacheSizeName(size int) string {
	if size == 0 {
		return "zero"
	}
	return "nonzero"
}

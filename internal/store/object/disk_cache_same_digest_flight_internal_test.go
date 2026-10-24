package object

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const diskCacheColdReaderCount = 32

type diskCacheRequestIDKey struct{}

type diskCacheOpenGate struct {
	release     chan struct{}
	started     chan struct{}
	sourceCtx   chan diskCacheSourceContext
	closedCount atomic.Int64
	releaseOnce sync.Once
}

type diskCacheSourceContext struct {
	id  int
	ctx context.Context
}

type diskCacheOpenGateReader struct {
	ctx       context.Context
	gate      *diskCacheOpenGate
	source    *bytes.Reader
	waitOnce  sync.Once
	waitError error
	closeOnce sync.Once
}

type diskCacheColdRequest struct {
	id     int
	reader io.ReadCloser
	info   *Info
	err    error
}

type diskCacheColdReadResult struct {
	body []byte
	err  error
}

func TestDiskCacheColdReadersShareStreamingFill(t *testing.T) {
	t.Run("followers stream independently and owner cancellation is isolated", testDiskCacheColdReaderFanIn)
	t.Run("short owner deadline does not cancel longer follower", testDiskCacheColdOwnerDeadlineIsolated)
	t.Run("invalidated fill cannot be overwritten before cleanup", testDiskCacheInvalidatedFillIsNotOverwritten)
	t.Run("last reader close cancels and cleans fill", testDiskCacheColdLastReaderCleanup)
}

func testDiskCacheColdReaderFanIn(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	gate := newDiskCacheOpenGate()
	t.Cleanup(gate.releaseBody)
	installDiskCacheOpenGate(t, backend, digest, payload, gate)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))

	ownerCtx, cancelOwner := newDiskCacheCallerContext(t, 0)
	owner, err := openDiskCacheColdRequest(ownerCtx, cache, digest)
	require.NoError(t, err, "open first cold request")
	ownerRead := readDiskCacheColdRequest(owner.reader)
	waitForDiskCacheSignal(t, gate.started)
	source := waitForDiskCacheSourceContext(t, gate)

	followers, cancels := openDiskCacheColdFollowers(t, cache, digest, 1, diskCacheColdReaderCount-1)
	t.Cleanup(func() {
		cancelOwner()
		for _, cancel := range cancels {
			cancel()
		}
	})
	if got := backend.gets.Load(); got != int64(1) {
		t.Errorf("canonical opens before releasing body gate = %d, want 1", got)
	}

	cancelOwner()
	ownerResult := receiveDiskCacheColdRead(t, ownerRead)
	require.ErrorIs(t, ownerResult.err, context.Canceled, "canceled owner reader")
	require.NoError(t, owner.reader.Close(), "close canceled owner reader")
	if err := source.ctx.Err(); err != nil {
		t.Errorf("canceling request %d canceled shared canonical stream: %v", source.id, err)
	}

	gate.releaseBody()
	assertDiskCacheColdFollowerBodies(t, followers, payload, digest)
	require.Equal(t, int64(1), backend.gets.Load(), "canonical opens after follower reads")
	require.Equal(t, 1, cache.Stats().Entries, "completed shared body entry count")
	require.Zero(t, cache.Stats().Fills, "completed shared body fill count")
	require.EqualValues(t, 1, gate.closedCount.Load(), "canonical source closes")
}

func testDiskCacheColdLastReaderCleanup(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	gate := newDiskCacheOpenGate()
	t.Cleanup(gate.releaseBody)
	installDiskCacheOpenGate(t, backend, digest, payload, gate)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))

	ownerCtx, ownerCancel := newDiskCacheCallerContext(t, 0)
	owner, err := openDiskCacheColdRequest(ownerCtx, cache, digest)
	require.NoError(t, err, "open cleanup owner")
	requests, cancels := openDiskCacheColdFollowers(t, cache, digest, 1, 1)
	requests = append([]diskCacheColdRequest{owner}, requests...)
	t.Cleanup(func() {
		ownerCancel()
		for _, cancel := range cancels {
			cancel()
		}
	})
	reads := startDiskCacheColdReads(requests)
	waitForDiskCacheSignal(t, gate.started)
	source := waitForDiskCacheSourceContext(t, gate)
	if got := backend.gets.Load(); got != int64(1) {
		t.Errorf("canonical opens while body gate is closed = %d, want 1", got)
	}

	require.NoError(t, requests[0].reader.Close(), "close first reader")
	if err := source.ctx.Err(); err != nil {
		t.Errorf("closing reader %d canceled stream with reader %d remaining: %v", requests[0].id, requests[1].id, err)
	}
	require.NoError(t, requests[1].reader.Close(), "close last reader")
	require.Error(t, waitForDiskCacheColdRead(t, reads[0]).err, "first blocked reader should stop")
	require.Error(t, waitForDiskCacheColdRead(t, reads[1]).err, "last blocked reader should stop")
	require.Error(t, source.ctx.Err(), "last reader close must cancel canonical stream")
	require.Equal(t, int64(1), backend.gets.Load(), "canonical opens after readers close")
	require.Equal(t, DiskCacheStats{}, cache.Stats(), "abandoned fill cleanup")
	assertDiskCacheTemporaryDirectoryEmpty(t, cache)
}

func openDiskCacheColdFollowers(
	t *testing.T,
	cache *DiskCacheStore,
	digest string,
	firstID, count int,
) ([]diskCacheColdRequest, []context.CancelFunc) {
	t.Helper()
	requests := make([]diskCacheColdRequest, count)
	cancels := make([]context.CancelFunc, count)
	results := make(chan diskCacheColdRequest, count)
	start := make(chan struct{})
	for offset := range count {
		id := firstID + offset
		ctx, cancel := newDiskCacheCallerContext(t, id)
		cancels[offset] = cancel
		go func() {
			<-start
			reader, info, err := cache.Get(ctx, digest, GetOptions{})
			results <- diskCacheColdRequest{id: id, reader: reader, info: info, err: err}
		}()
	}
	close(start)
	for range count {
		request := receiveDiskCacheColdRequest(t, results)
		if request.err != nil {
			t.Fatalf("open concurrent cold reader: %v", request.err)
		}
		requests[request.id-firstID] = request
	}
	return requests, cancels
}

func openDiskCacheColdRequest(
	ctx context.Context,
	cache *DiskCacheStore,
	digest string,
) (diskCacheColdRequest, error) {
	reader, info, err := cache.Get(ctx, digest, GetOptions{})
	return diskCacheColdRequest{id: 0, reader: reader, info: info, err: err}, err
}

func newDiskCacheCallerContext(t *testing.T, id int) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), diskCacheRequestIDKey{}, id))
	return ctx, cancel
}

func installDiskCacheOpenGate(t *testing.T, backend *diskCacheTestBackend, digest string, payload []byte, gate *diskCacheOpenGate) {
	t.Helper()
	info, err := backend.Stat(context.Background(), digest)
	require.NoError(t, err, "stat seeded object")
	backend.get = func(ctx context.Context, requested string, _ GetOptions) (io.ReadCloser, *Info, error) {
		if requested != digest {
			return nil, nil, fmt.Errorf("unexpected digest %q", requested)
		}
		id, ok := ctx.Value(diskCacheRequestIDKey{}).(int)
		if !ok {
			id = 0
		}
		gate.sourceCtx <- diskCacheSourceContext{id: id, ctx: ctx}
		return &diskCacheOpenGateReader{ctx: ctx, gate: gate, source: bytes.NewReader(payload)}, info, nil
	}
}

func newDiskCacheOpenGate() *diskCacheOpenGate {
	return &diskCacheOpenGate{
		release:   make(chan struct{}),
		started:   make(chan struct{}, 1),
		sourceCtx: make(chan diskCacheSourceContext, diskCacheColdReaderCount),
	}
}

func (r *diskCacheOpenGateReader) Read(buffer []byte) (int, error) {
	r.waitOnce.Do(func() {
		select {
		case r.gate.started <- struct{}{}:
		default:
		}
		select {
		case <-r.gate.release:
		case <-r.ctx.Done():
			r.waitError = fmt.Errorf("gated source canceled: %w", r.ctx.Err())
		}
	})
	if r.waitError != nil {
		return 0, r.waitError
	}
	n, err := r.source.Read(buffer)
	if err == io.EOF {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read disk cache test source: %w", err)
	}
	return n, nil
}

func (r *diskCacheOpenGateReader) Close() error {
	r.closeOnce.Do(func() { r.gate.closedCount.Add(1) })
	return nil
}

func (g *diskCacheOpenGate) releaseBody() {
	g.releaseOnce.Do(func() { close(g.release) })
}

func waitForDiskCacheSourceContext(t *testing.T, gate *diskCacheOpenGate) diskCacheSourceContext {
	t.Helper()
	select {
	case ctx := <-gate.sourceCtx:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for canonical source open")
		return diskCacheSourceContext{}
	}
}

func receiveDiskCacheColdRequest(t *testing.T, results <-chan diskCacheColdRequest) diskCacheColdRequest {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent Get")
		return diskCacheColdRequest{}
	}
}

func readDiskCacheColdRequest(reader io.ReadCloser) <-chan diskCacheColdReadResult {
	result := make(chan diskCacheColdReadResult, 1)
	go func() {
		body, err := io.ReadAll(reader)
		result <- diskCacheColdReadResult{body: body, err: err}
	}()
	return result
}

func receiveDiskCacheColdRead(t *testing.T, results <-chan diskCacheColdReadResult) diskCacheColdReadResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cold reader")
		return diskCacheColdReadResult{}
	}
}

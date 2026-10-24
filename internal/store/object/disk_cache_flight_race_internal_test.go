package object

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type diskCacheAfterFuncGateContext struct {
	context.Context
	entered chan struct{}
	release chan struct{}
}

func (ctx *diskCacheAfterFuncGateContext) AfterFunc(callback func()) func() bool {
	stop := context.AfterFunc(ctx.Context, callback)
	close(ctx.entered)
	<-ctx.release
	return stop
}

func (ctx *diskCacheAfterFuncGateContext) Value(any) any { return nil }

func TestDiskCacheCanceledReaderIsNotRegisteredAfterClose(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	gate := newDiskCacheOpenGate()
	t.Cleanup(gate.releaseBody)
	installDiskCacheOpenGate(t, backend, digest, payload, gate)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))

	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	caller := &diskCacheAfterFuncGateContext{
		Context: parent,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	result := make(chan diskCacheColdRequest, 1)
	go func() {
		reader, info, err := cache.Get(caller, digest, GetOptions{})
		result <- diskCacheColdRequest{reader: reader, info: info, err: err}
	}()
	waitForDiskCacheSignal(t, gate.started)
	waitForDiskCacheSignal(t, caller.entered)
	cancel()
	<-caller.Done()
	close(caller.release)
	request := receiveDiskCacheColdRequest(t, result)
	require.Nil(t, request.reader, "canceled Get must not return a closed reader")
	require.ErrorIs(t, request.err, context.Canceled, "canceled Get result")
	cache.mu.Lock()
	registered := len(cache.flightReaders)
	cache.mu.Unlock()
	require.Zero(t, registered, "canceled reader registration")
	require.NoError(t, cache.Close(), "close canceled flight")
}

func testDiskCacheColdOwnerDeadlineIsolated(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	gate := newDiskCacheOpenGate()
	t.Cleanup(gate.releaseBody)
	installDiskCacheOpenGate(t, backend, digest, payload, gate)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))

	ownerCtx, cancelOwner := context.WithTimeout(t.Context(), time.Second)
	defer cancelOwner()
	owner, err := openDiskCacheColdRequest(ownerCtx, cache, digest)
	require.NoError(t, err, "open short-deadline owner")
	ownerRead := readDiskCacheColdRequest(owner.reader)
	waitForDiskCacheSignal(t, gate.started)
	source := waitForDiskCacheSourceContext(t, gate)

	followerCtx, cancelFollower := newDiskCacheCallerContext(t, 1)
	defer cancelFollower()
	follower, err := openDiskCacheColdRequest(followerCtx, cache, digest)
	require.NoError(t, err, "join long-lived follower")
	followerRead := readDiskCacheColdRequest(follower.reader)

	select {
	case <-ownerCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("owner deadline did not expire")
	}
	ownerResult := receiveDiskCacheColdRead(t, ownerRead)
	require.ErrorIs(t, ownerResult.err, context.DeadlineExceeded, "short owner deadline")
	require.NoError(t, owner.reader.Close(), "close expired owner")
	require.NoError(t, source.ctx.Err(), "owner deadline must not cancel shared source")

	gate.releaseBody()
	followerResult := receiveDiskCacheColdRead(t, followerRead)
	require.NoError(t, followerResult.err, "long-lived follower read")
	require.Equal(t, payload, followerResult.body, "follower body")
	require.Equal(t, digest, digestForTest(followerResult.body), "follower digest")
	require.NoError(t, follower.reader.Close(), "close follower")
	require.Equal(t, int64(1), backend.gets.Load(), "shared canonical opens")
}

func testDiskCacheInvalidatedFillIsNotOverwritten(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	gate := newDiskCacheOpenGate()
	t.Cleanup(gate.releaseBody)
	installDiskCacheOpenGate(t, backend, digest, payload, gate)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))

	owner, err := openDiskCacheColdRequest(t.Context(), cache, digest)
	require.NoError(t, err, "open fill owner")
	ownerRead := readDiskCacheColdRequest(owner.reader)
	waitForDiskCacheSignal(t, gate.started)
	cache.mu.Lock()
	oldFill := cache.fills[digest]
	cache.mu.Unlock()
	require.NotNil(t, oldFill, "active fill registration")

	if evictErr := cache.Evict(t.Context(), digest); evictErr != nil {
		t.Fatalf("invalidate active fill: %v", evictErr)
	}
	bypass, _, err := cache.Get(t.Context(), digest, GetOptions{})
	require.NoError(t, err, "read while invalidated fill is still active")
	require.Equal(t, int64(2), backend.gets.Load(), "invalidated fill bypasses without replacement fill")
	cache.mu.Lock()
	retained := cache.fills[digest]
	cache.mu.Unlock()
	require.Same(t, oldFill, retained, "old fill remains the indexed digest fill")
	require.NoError(t, bypass.Close(), "close bypass reader")

	gate.releaseBody()
	ownerResult := receiveDiskCacheColdRead(t, ownerRead)
	require.NoError(t, ownerResult.err, "read invalidated fill owner")
	require.Equal(t, payload, ownerResult.body, "owner body")
	require.Equal(t, digest, digestForTest(ownerResult.body), "owner digest")
	require.NoError(t, owner.reader.Close(), "close owner")
	require.Zero(t, cache.Stats().Fills, "invalidated fill cleanup")
	require.Zero(t, cache.Stats().ReservedBytes, "invalidated fill reservation cleanup")
	cache.mu.Lock()
	_, hasKeyState := cache.keyStates[digest]
	cache.mu.Unlock()
	require.False(t, hasKeyState, "invalidated fill key token cleanup")
}

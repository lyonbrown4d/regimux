package object

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiskCacheTerminalFillCannotBeRejoined(t *testing.T) {
	t.Run("zero references", func(t *testing.T) {
		assertDiskCacheFillCannotBeRejoined(t, 0, false)
	})
	t.Run("finalizing", func(t *testing.T) {
		assertDiskCacheFillCannotBeRejoined(t, 1, true)
	})
}

func assertDiskCacheFillCannotBeRejoined(t *testing.T, refs int, finalizing bool) {
	t.Helper()
	backend, digest, _ := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	fill := &diskCacheFill{
		digest: digest, refs: refs, finalizing: finalizing,
		ready: make(chan struct{}), done: make(chan struct{}), refsDone: make(chan struct{}),
	}
	cache.mu.Lock()
	cache.fills[digest] = fill
	cache.mu.Unlock()

	joinedFill, leader, shared, err := cache.joinDiskCacheFlight(t.Context(), digest)
	cache.mu.Lock()
	delete(cache.fills, digest)
	cache.mu.Unlock()
	require.NoError(t, err, "join after fill terminal transition")
	require.False(t, shared, "terminal fill must not be joined")
	require.False(t, leader, "terminal fill must not elect a new leader")
	require.Nil(t, joinedFill, "terminal fill admission result")
}

package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestDiskCacheEvictAndDeleteInvalidateLocalCopy(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	readCachedObject(t, cache, digest, payload)
	if err := cache.Evict(t.Context(), digest); err != nil {
		t.Fatalf("evict local copy: %v", err)
	}
	readCachedObject(t, cache, digest, payload)
	if got := backend.gets.Load(); got != 2 {
		t.Fatalf("backend Get calls after Evict = %d, want 2", got)
	}
	if err := cache.Delete(t.Context(), digest); err != nil {
		t.Fatalf("delete canonical object: %v", err)
	}
	if exists, err := backend.Exists(t.Context(), digest); err != nil || exists {
		t.Fatalf("canonical Exists after Delete = %v, %v; want false, nil", exists, err)
	}
	if stats := cache.Stats(); stats.Entries != 0 {
		t.Fatalf("Delete retained local copy: %+v", stats)
	}
}

func TestDiskCacheDeleteDuringFillCannotResurrectEntry(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	started := make(chan struct{})
	release := make(chan struct{})
	backend.get = func(ctx context.Context, requested string, opts GetOptions) (io.ReadCloser, *Info, error) {
		reader, info, err := backend.getCanonical(ctx, requested, opts)
		if err != nil {
			return nil, nil, err
		}
		return &diskCacheGateReader{ctx: ctx, source: reader, started: started, release: release}, info, nil
	}
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	reader, _, err := cache.Get(t.Context(), digest, GetOptions{})
	if err != nil {
		t.Fatalf("start filling object: %v", err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, reader)
		readDone <- readErr
	}()
	waitForDiskCacheSignal(t, started)
	if err := cache.Delete(t.Context(), digest); err != nil {
		t.Fatalf("delete during fill: %v", err)
	}
	close(release)
	if err := <-readDone; err != nil {
		t.Fatalf("finish in-flight reader: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close in-flight reader: %v", err)
	}
	if exists, err := backend.Exists(t.Context(), digest); err != nil || exists {
		t.Fatalf("canonical Exists after Delete/fill = %v, %v; want false, nil", exists, err)
	}
	if stats := cache.Stats(); stats.Entries != 0 || stats.Fills != 0 {
		t.Fatalf("Delete allowed fill to resurrect object: %+v (payload %d bytes)", stats, len(payload))
	}
}

func TestDiskCacheDeleteDuringCanonicalOpenCannotStartLateFill(t *testing.T) {
	backend, digest, _ := newDiskCacheTestBackend(t)
	opened := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	backend.get = func(ctx context.Context, requested string, opts GetOptions) (io.ReadCloser, *Info, error) {
		close(opened)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("backend Get canceled: %w", ctx.Err())
		}
		return backend.getCanonical(ctx, requested, opts)
	}
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	getDone := make(chan error, 1)
	go func() {
		reader, _, err := cache.Get(t.Context(), digest, GetOptions{})
		if reader != nil {
			err = errors.Join(err, reader.Close())
		}
		getDone <- err
	}()
	waitForDiskCacheSignal(t, opened)
	if err := cache.Delete(t.Context(), digest); err != nil {
		t.Fatalf("delete during canonical Get: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-getDone; !errors.Is(err, ErrNotFound) {
		t.Fatalf("in-flight Get after Delete = %v, want ErrNotFound", err)
	}
	if stats := cache.Stats(); stats.Entries != 0 || stats.Fills != 0 || stats.ReservedBytes != 0 {
		t.Fatalf("late Get created cache fill after Delete: %+v", stats)
	}
}

func TestDiskCacheCallerCancellationStopsFill(t *testing.T) {
	backend, digest, _ := newDiskCacheTestBackend(t)
	started := make(chan struct{})
	release := make(chan struct{})
	backend.get = func(ctx context.Context, requested string, opts GetOptions) (io.ReadCloser, *Info, error) {
		reader, info, err := backend.getCanonical(ctx, requested, opts)
		if err != nil {
			return nil, nil, err
		}
		return &diskCacheGateReader{ctx: ctx, source: reader, started: started, release: release}, info, nil
	}
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader, _, err := cache.Get(ctx, digest, GetOptions{})
	if err != nil {
		t.Fatalf("start cancellable fill: %v", err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(io.Discard, reader)
		readDone <- readErr
	}()
	waitForDiskCacheSignal(t, started)
	cancel()
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("read after caller cancellation succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("read did not stop after caller cancellation")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close canceled reader: %v", err)
	}
	if stats := cache.Stats(); stats.Entries != 0 || stats.Fills != 0 {
		t.Fatalf("caller cancellation left a cache entry/fill: %+v", stats)
	}
}

func TestDiskCacheCapacityAndTTLBoundEntries(t *testing.T) {
	backend, firstDigest, first := newDiskCacheTestBackend(t)
	second := []byte("second canonical object")
	secondDigest := digestForTest(second)
	if _, err := backend.canonical.Put(t.Context(), secondDigest, bytes.NewReader(second), PutOptions{}); err != nil {
		t.Fatalf("seed second object: %v", err)
	}
	opts := diskCacheTestOptions(t)
	opts.MaxEntries = 1
	opts.TTL = 5 * time.Millisecond
	cache := newTestDiskCache(t, backend, opts)
	readCachedObject(t, cache, firstDigest, first)
	readCachedObject(t, cache, secondDigest, second)
	if stats := cache.Stats(); stats.Entries != 1 {
		t.Fatalf("entry cap not enforced: %+v", stats)
	}
	cache.mu.Lock()
	cache.entries[secondDigest].lastAccess = time.Now().Add(-opts.TTL - time.Second)
	cache.mu.Unlock()
	readCachedObject(t, cache, secondDigest, second)
	if got := backend.gets.Load(); got != 3 {
		t.Fatalf("backend Gets after LRU eviction and TTL expiry = %d, want 3", got)
	}
}

func TestDiskCacheCorruptLocalFileFallsBackToCanonicalStore(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	readCachedObject(t, cache, digest, payload)
	canonical, err := backend.canonical.Stat(t.Context(), digest)
	if err != nil {
		t.Fatalf("stat canonical object: %v", err)
	}
	reader, info, err := cache.Get(t.Context(), digest, GetOptions{})
	if err != nil {
		t.Fatalf("open warm object: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close warm object: %v", err)
	}
	if info.Path == canonical.Path {
		t.Fatalf("warm Get returned canonical path %q, want local path", info.Path)
	}
	if err := os.WriteFile(info.Path, []byte("corrupt local bytes"), 0o600); err != nil {
		t.Fatalf("corrupt local cache file: %v", err)
	}
	readCachedObject(t, cache, digest, payload)
	if got := backend.gets.Load(); got != 2 {
		t.Fatalf("backend Get calls after local corruption = %d, want 2", got)
	}
}

func TestDiskCacheSameKeyReadersAreIndependent(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	readCachedObject(t, cache, digest, payload)

	const readers = 8
	var wait sync.WaitGroup
	errs := make(chan error, readers)
	for range readers {
		wait.Go(func() {
			body, _, err := cache.Get(t.Context(), digest, GetOptions{})
			if err != nil {
				errs <- err
				return
			}
			errs <- readAndCloseDiskCacheReader(body, payload)
		})
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "independent reader")
	}
	require.EqualValues(t, 1, backend.gets.Load(), "backend Get calls for hot concurrent reads")
}

func TestDiskCacheStatAndListingRemainCanonical(t *testing.T) {
	backend, digest, payload := newDiskCacheTestBackend(t)
	cache := newTestDiskCache(t, backend, diskCacheTestOptions(t))
	readCachedObject(t, cache, digest, payload)

	info, err := cache.Stat(t.Context(), digest)
	if err != nil {
		t.Fatalf("canonical Stat: %v", err)
	}
	if info.Path == "" {
		t.Fatal("canonical Stat returned empty path")
	}
	listed, err := cache.ListObjects(t.Context())
	if err != nil {
		t.Fatalf("canonical ListObjects: %v", err)
	}
	if len(listed) != 1 || listed[0].Digest != digest {
		t.Fatalf("canonical listing = %+v, want only %s", listed, digest)
	}
}

func digestForTest(body []byte) string {
	return ocidigest.FromBytes(body).String()
}

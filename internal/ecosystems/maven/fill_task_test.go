//nolint:testpackage // Tests exercise private fill-task ownership and lifecycle state.
package maven

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/panjf2000/ants/v2"
)

func TestMavenFillPoolJoinsTaskAndRetainsReplayUntilClientsRelease(t *testing.T) {
	pool := newTestMavenFillPool(t, 2)
	prepared, path := newTestPreparedMavenResponse(t)
	started := make(chan struct{})
	finish := make(chan struct{})
	stopWork := testSignalCloser(finish)
	t.Cleanup(stopWork)
	var runs atomic.Int32
	work := func(_ context.Context, task *pendingMavenFill) error {
		runs.Add(1)
		close(started)
		if !task.publish(prepared) {
			return errors.New("fill rejected prepared response")
		}
		<-finish
		return nil
	}

	key := artifactcache.Key{Alias: "central", Repository: "org/example", Reference: "1.0"}
	first, firstOwner := acquireTestMavenFill(t, pool, key, work)
	releaseFirst := testMavenFillRelease(t, first)
	waitMavenFillSignal(t, started)
	second, secondOwner := acquireTestMavenFill(t, pool, key, work)
	releaseSecond := testMavenFillRelease(t, second)

	assertMavenFillJoin(t, first, firstOwner, second, secondOwner, runs.Load())
	assertMavenFillPublished(t, first, second, prepared)
	releaseFirst()
	assertReplayExists(t, path)
	releaseSecond()
	assertReplayExists(t, path)
	stopWork()
	waitForMavenFill(t, func() bool { return replayWasRemoved(path) })
}

func TestMavenFillPoolLastReleaseCancelsUnpublishedTask(t *testing.T) {
	pool := newTestMavenFillPool(t, 1)
	started := make(chan struct{})
	canceled := make(chan struct{})
	fill, _ := acquireTestMavenFill(t, pool, artifactcache.Key{Alias: "central", Repository: "org/example", Reference: "2"}, func(ctx context.Context, _ *pendingMavenFill) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		return ctx.Err()
	})
	release := testMavenFillRelease(t, fill)
	waitMavenFillSignal(t, started)
	release()
	waitMavenFillSignal(t, canceled)
}

func TestMavenFillPoolPanicUnblocksWaiters(t *testing.T) {
	pool := newTestMavenFillPool(t, 1)
	fill, _ := acquireTestMavenFill(t, pool, artifactcache.Key{Alias: "central", Repository: "org/example", Reference: "3"}, func(context.Context, *pendingMavenFill) error {
		panic("unexpected fill panic")
	})
	testMavenFillRelease(t, fill)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := fill.wait(ctx); err == nil {
		t.Fatal("wait error = nil, want panic reported")
	}
}

func TestMavenFillWaitHonorsCancellationAfterPublish(t *testing.T) {
	pool := newTestMavenFillPool(t, 1)
	prepared, _ := newTestPreparedMavenResponse(t)
	started := make(chan struct{})
	finish := make(chan struct{})
	stopWork := testSignalCloser(finish)
	t.Cleanup(stopWork)
	fill, _ := acquireTestMavenFill(t, pool, artifactcache.Key{Alias: "central", Repository: "org/example", Reference: "3a"}, func(_ context.Context, task *pendingMavenFill) error {
		if !task.publish(prepared) {
			return errors.New("fill rejected prepared response")
		}
		close(started)
		<-finish
		return nil
	})
	testMavenFillRelease(t, fill)
	waitMavenFillSignal(t, started)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fill.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context.Canceled", err)
	}
	stopWork()
}

func TestMavenFillPoolPreservesNonblockingOverload(t *testing.T) {
	pool := newTestMavenFillPool(t, 1)
	started := make(chan struct{})
	finish := make(chan struct{})
	stopWork := testSignalCloser(finish)
	t.Cleanup(stopWork)
	fill, _ := acquireTestMavenFill(t, pool, artifactcache.Key{Alias: "central", Repository: "org/example", Reference: "4"}, func(context.Context, *pendingMavenFill) error {
		close(started)
		<-finish
		return nil
	})
	testMavenFillRelease(t, fill)
	waitMavenFillSignal(t, started)
	_, _, err := pool.acquire(context.Background(), artifactcache.Key{Alias: "central", Repository: "org/example", Reference: "5"}, func(context.Context, *pendingMavenFill) error { return nil })
	if !errors.Is(err, ants.ErrPoolOverload) {
		t.Fatalf("acquire error = %v, want ants.ErrPoolOverload", err)
	}
	stopWork()
}

func TestNewMavenFillPoolRejectsUnboundedCapacity(t *testing.T) {
	if _, err := newMavenFillPool(0); err == nil {
		t.Fatal("newMavenFillPool(0) error = nil, want bounded-capacity error")
	}
}

func TestMavenFillPoolRejectsAcquireAfterClose(t *testing.T) {
	pool := newTestMavenFillPool(t, 1)
	if err := pool.close(); err != nil {
		t.Fatal(err)
	}
	_, _, err := pool.acquire(context.Background(), artifactcache.Key{Alias: "central", Repository: "org/example", Reference: "6"}, func(context.Context, *pendingMavenFill) error { return nil })
	if err == nil {
		t.Fatal("acquire after close error = nil, want failure")
	}
}

func newTestMavenFillPool(t *testing.T, concurrency int) *mavenFillPool {
	t.Helper()
	pool, err := newMavenFillPool(concurrency)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := pool.close(); closeErr != nil {
			t.Errorf("close maven fill pool: %v", closeErr)
		}
	})
	return pool
}

func newTestPreparedMavenResponse(t *testing.T) (*preparedMavenResponse, string) {
	t.Helper()
	path := t.TempDir() + "/replay"
	if err := os.WriteFile(path, []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &preparedMavenResponse{replay: &replayableBody{path: path}}, path
}

func acquireTestMavenFill(t *testing.T, pool *mavenFillPool, key artifactcache.Key, work func(context.Context, *pendingMavenFill) error) (*pendingMavenFill, bool) {
	t.Helper()
	fill, owner, err := pool.acquire(context.Background(), key, work)
	if err != nil {
		t.Fatal(err)
	}
	return fill, owner
}

func testMavenFillRelease(t *testing.T, fill *pendingMavenFill) func() {
	t.Helper()
	var once sync.Once
	release := func() { once.Do(fill.release) }
	t.Cleanup(release)
	return release
}

func testSignalCloser(signal chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(signal) }) }
}

func assertMavenFillJoin(t *testing.T, first *pendingMavenFill, firstOwner bool, second *pendingMavenFill, secondOwner bool, runs int32) {
	t.Helper()
	if !firstOwner || secondOwner || first != second || runs != 1 {
		t.Fatalf("join state: first owner=%v, second owner=%v, same task=%v, work runs=%d", firstOwner, secondOwner, first == second, runs)
	}
}

func assertMavenFillPublished(t *testing.T, first, second *pendingMavenFill, prepared *preparedMavenResponse) {
	t.Helper()
	for _, fill := range []*pendingMavenFill{first, second} {
		got, err := fill.wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got != prepared {
			t.Fatal("wait returned a different prepared response")
		}
	}
}

func assertReplayExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("replay removed before all owners finished: %v", err)
	}
}

func replayWasRemoved(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

func waitForMavenFill(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true before timeout")
}

func waitMavenFillSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("maven fill did not reach expected state before timeout")
	}
}

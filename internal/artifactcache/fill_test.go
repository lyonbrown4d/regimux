package artifactcache_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

//nolint:funlen,gocognit // This concurrent lease test keeps setup, blocking, release, and result assertions together.
func TestCoalesceFillUsesSharedLeaseAcrossTrackers(t *testing.T) {
	ctx := context.Background()
	locker := newFakeFillLocker()
	left := artifactcache.NewFillTracker(
		artifactcache.WithFillLocker(locker),
		artifactcache.WithFillPollInterval(5*time.Millisecond),
		artifactcache.WithFillLeaseTTL(time.Second),
	)
	right := artifactcache.NewFillTracker(
		artifactcache.WithFillLocker(locker),
		artifactcache.WithFillPollInterval(5*time.Millisecond),
		artifactcache.WithFillLeaseTTL(time.Second),
	)
	key := artifactcache.Key{Alias: "npmjs", Repository: "left-pad", Reference: "metadata"}

	var upstreamCalls atomic.Int64
	var cached atomic.Bool
	var waitErr error
	var fillErr error
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	wait := func() (string, bool, error) {
		if waitErr != nil {
			return "", true, waitErr
		}
		if cached.Load() {
			return "cached", true, nil
		}
		return "", false, nil
	}
	fill := func() (string, error) {
		if upstreamCalls.Add(1) == 1 {
			close(started)
		}
		<-release
		if fillErr != nil {
			return "", fillErr
		}
		cached.Store(true)
		return "origin", nil
	}

	leftResult := make(chan fillResult, 1)
	go func() {
		value, err := artifactcache.CoalesceFill(ctx, left, key, wait, fill)
		leftResult <- fillResult{value: value, err: err}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first fill did not start")
	}

	rightResult := make(chan fillResult, 1)
	go func() {
		value, err := artifactcache.CoalesceFill(ctx, right, key, wait, fill)
		rightResult <- fillResult{value: value, err: err}
	}()

	time.Sleep(50 * time.Millisecond)
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls while distributed lease is held = %d, want 1", got)
	}
	releaseOnce.Do(func() { close(release) })

	assertFillResult(t, leftResult, "origin")
	assertFillResult(t, rightResult, "cached")
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

type fillResult struct {
	value string
	err   error
}

func assertFillResult(t *testing.T, results <-chan fillResult, want string) {
	t.Helper()
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatalf("coalesce fill returned error: %v", result.err)
		}
		if result.value != want {
			t.Fatalf("coalesce fill value = %q, want %q", result.value, want)
		}
	case <-time.After(time.Second):
		t.Fatal("coalesce fill did not return")
	}
}

type fakeFillLocker struct {
	mu    sync.Mutex
	locks map[string]string
	next  atomic.Int64
}

func newFakeFillLocker() *fakeFillLocker {
	return &fakeFillLocker{locks: map[string]string{}}
}

func (l *fakeFillLocker) AcquireLease(
	_ context.Context,
	key string,
	_ time.Duration,
) (artifactcache.FillLease, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.locks[key]; ok {
		return nil, false, nil
	}
	token := l.next.Add(1)
	value := time.Unix(0, token).String()
	l.locks[key] = value
	return &fakeFillLease{locker: l, key: key, token: value}, true, nil
}

type fakeFillLease struct {
	locker *fakeFillLocker
	key    string
	token  string
}

func (l *fakeFillLease) Release(context.Context) error {
	l.locker.mu.Lock()
	defer l.locker.mu.Unlock()
	if l.locker.locks[l.key] == l.token {
		delete(l.locker.locks, l.key)
	}
	return nil
}

func (l *fakeFillLease) Extend(context.Context, time.Duration) error {
	return nil
}

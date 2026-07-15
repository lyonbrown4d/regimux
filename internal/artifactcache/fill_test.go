package artifactcache_test

import (
	"context"
	"github.com/lyonbrown4d/regimux/internal/testkit"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

func TestCoalesceFillUsesSharedLeaseAcrossTrackers(t *testing.T) {
	fixture := newSharedLeaseFixture(t)
	leftResult := fixture.start(fixture.left)
	testkit.WaitForSignal(t, fixture.started)

	rightResult := fixture.start(fixture.right)
	testkit.WaitForSignal(t, fixture.locker.contended)
	if got := fixture.upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls while distributed lease is held = %d, want 1", got)
	}

	fixture.releaseFill()
	assertFillResult(t, leftResult, "origin")
	assertFillResult(t, rightResult, "cached")
	if got := fixture.upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

type sharedLeaseFixture struct {
	ctx           context.Context
	key           artifactcache.Key
	locker        *fakeFillLocker
	left          *artifactcache.FillTracker
	right         *artifactcache.FillTracker
	upstreamCalls atomic.Int64
	cached        atomic.Bool
	started       chan struct{}
	release       chan struct{}
	releaseOnce   sync.Once
}

func newSharedLeaseFixture(t *testing.T) *sharedLeaseFixture {
	t.Helper()
	locker := newFakeFillLocker()
	fixture := &sharedLeaseFixture{
		ctx:     context.Background(),
		key:     artifactcache.Key{Alias: "npmjs", Repository: "left-pad", Reference: "metadata"},
		locker:  locker,
		left:    newLeaseTracker(locker),
		right:   newLeaseTracker(locker),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(fixture.releaseFill)
	return fixture
}

func newLeaseTracker(locker artifactcache.FillLocker) *artifactcache.FillTracker {
	return artifactcache.NewFillTracker(
		artifactcache.WithFillLocker(locker),
		artifactcache.WithFillPollInterval(5*time.Millisecond),
		artifactcache.WithFillLeaseTTL(time.Second),
	)
}

func (f *sharedLeaseFixture) start(tracker *artifactcache.FillTracker) <-chan fillResult {
	results := make(chan fillResult, 1)
	go func() {
		value, err := artifactcache.CoalesceFill(f.ctx, tracker, f.key, f.wait, f.fill)
		results <- fillResult{value: value, err: err}
	}()
	return results
}

func (f *sharedLeaseFixture) wait() (string, bool, error) {
	if f.cached.Load() {
		return "cached", true, nil
	}
	return "", false, nil
}

func (f *sharedLeaseFixture) fill() (string, error) {
	if f.upstreamCalls.Add(1) == 1 {
		close(f.started)
	}
	<-f.release
	f.cached.Store(true)
	return "origin", nil
}

func (f *sharedLeaseFixture) releaseFill() {
	f.releaseOnce.Do(func() { close(f.release) })
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
	mu            sync.Mutex
	locks         map[string]string
	next          atomic.Int64
	contended     chan struct{}
	contendedOnce sync.Once
}

func newFakeFillLocker() *fakeFillLocker {
	return &fakeFillLocker{
		locks:     map[string]string{},
		contended: make(chan struct{}),
	}
}

func (l *fakeFillLocker) AcquireLease(
	_ context.Context,
	key string,
	_ time.Duration,
) (artifactcache.FillLease, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.locks[key]; ok {
		l.contendedOnce.Do(func() { close(l.contended) })
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

package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestBlobFillLeaseRenewalStopsOnReleaseWithoutWaitingForTTL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := newRecordingLeaseRenewScheduler()
	lease := &recordingLease{}
	proxy := blobProxy{leaseScheduler: scheduler}

	release := proxy.startBlobFillLease(ctx, BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, lease)
	scheduler.waitSubmitted(t)

	cancel()
	release()
	release()
	scheduler.waitDone(t)

	if got := scheduler.calls(); got != 1 {
		t.Fatalf("scheduler calls = %d, want 1", got)
	}
	if got := lease.extendCalls(); got != 0 {
		t.Fatalf("lease extend calls = %d, want 0 before renewal TTL", got)
	}
	if got := lease.releaseCalls(); got != 1 {
		t.Fatalf("lease release calls = %d, want 1", got)
	}
	if err := lease.releaseContextError(); err != nil {
		t.Fatalf("release context err = %v, want nil after request context cancellation", err)
	}
	deadline, ok := lease.releaseContextDeadline()
	if !ok {
		t.Fatal("release context had no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > blobFillLeaseReleaseTimeout+100*time.Millisecond {
		t.Fatalf("release context deadline remaining = %s, want within %s", remaining, blobFillLeaseReleaseTimeout)
	}
}

type recordingLeaseRenewScheduler struct {
	mu        sync.Mutex
	submitted int
	once      sync.Once
	ready     chan struct{}
	done      chan struct{}
}

func newRecordingLeaseRenewScheduler() *recordingLeaseRenewScheduler {
	return &recordingLeaseRenewScheduler{
		ready: make(chan struct{}),
		done:  make(chan struct{}),
	}
}

func (s *recordingLeaseRenewScheduler) Submit(task func()) error {
	s.mu.Lock()
	s.submitted++
	s.mu.Unlock()
	s.once.Do(func() { close(s.ready) })
	go func() {
		defer close(s.done)
		task()
	}()
	return nil
}

func (s *recordingLeaseRenewScheduler) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.submitted
}

func (s *recordingLeaseRenewScheduler) waitSubmitted(t *testing.T) {
	t.Helper()
	select {
	case <-s.ready:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("lease renewal task was not submitted")
	}
}

func (s *recordingLeaseRenewScheduler) waitDone(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("lease renewal task did not stop after release")
	}
}

type recordingLease struct {
	mu              sync.Mutex
	releases        int
	extends         int
	releaseErr      error
	releaseDeadline time.Time
	hasDeadline     bool
}

func (l *recordingLease) Release(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releases++
	l.releaseErr = ctx.Err()
	l.releaseDeadline, l.hasDeadline = ctx.Deadline()
	return nil
}

func (l *recordingLease) Extend(context.Context, time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.extends++
	return nil
}

func (l *recordingLease) releaseCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.releases
}

func (l *recordingLease) extendCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.extends
}

func (l *recordingLease) releaseContextError() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.releaseErr
}

func (l *recordingLease) releaseContextDeadline() (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.releaseDeadline, l.hasDeadline
}

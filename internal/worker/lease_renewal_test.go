package worker_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/panjf2000/ants/v2"
)

func TestLeaseRenewalStopCancelsRetryWhenPoolIsFull(t *testing.T) {
	pools := newLeaseTestPools(t)
	firstEntered, releaseFirst := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	firstStop := worker.StartLeaseRenewal(context.Background(), pools.LeasePool(), 5*time.Millisecond, func(context.Context) {
		enteredOnce.Do(func() { close(firstEntered) })
		<-releaseFirst
	}, nil)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseFirst) })
		firstStop()
	})
	waitLeaseSignal(t, firstEntered)

	scheduler := &observedLeaseScheduler{pool: pools.LeasePool(), overloaded: make(chan struct{}, 1)}
	var secondRuns atomic.Int32
	secondStop := worker.StartLeaseRenewal(context.Background(), scheduler, 5*time.Millisecond, func(context.Context) {
		secondRuns.Add(1)
	}, nil)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseFirst) })
		firstStop()
		secondStop()
	})
	waitLeaseSignal(t, scheduler.overloaded)

	stopped := make(chan struct{})
	go func() { secondStop(); close(stopped) }()
	waitLeaseSignal(t, stopped)
	releaseOnce.Do(func() { close(releaseFirst) })
	firstStop()
	if got := secondRuns.Load(); got != 0 {
		t.Fatalf("second renewal calls = %d, want 0 after stop while pool is full", got)
	}
}

func TestLeaseRenewalRetriesAfterPoolCapacityReturns(t *testing.T) {
	pools := newLeaseTestPools(t)
	firstEntered, releaseFirst := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	firstStop := worker.StartLeaseRenewal(context.Background(), pools.LeasePool(), 5*time.Millisecond, func(context.Context) {
		enteredOnce.Do(func() { close(firstEntered) })
		<-releaseFirst
	}, nil)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseFirst) })
		firstStop()
	})
	waitLeaseSignal(t, firstEntered)

	scheduler := &observedLeaseScheduler{pool: pools.LeasePool(), overloaded: make(chan struct{}, 1)}
	secondEntered := make(chan struct{})
	var secondOnce sync.Once
	secondStop := worker.StartLeaseRenewal(context.Background(), scheduler, 5*time.Millisecond, func(context.Context) {
		secondOnce.Do(func() { close(secondEntered) })
	}, nil)
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseFirst) })
		firstStop()
		secondStop()
	})
	waitLeaseSignal(t, scheduler.overloaded)
	releaseOnce.Do(func() { close(releaseFirst) })
	waitLeaseSignal(t, secondEntered)
	secondStop()
	firstStop()
}

func TestLeaseRenewalStopJoinsContextAwareRenewalAndIsIdempotent(t *testing.T) {
	entered, canceled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	stop := worker.StartLeaseRenewal(context.Background(), inlineLeaseScheduler{}, 5*time.Millisecond, func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-finish
	}, nil)
	t.Cleanup(func() {
		finishOnce.Do(func() { close(finish) })
		stop()
	})
	waitLeaseSignal(t, entered)
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	waitLeaseSignal(t, canceled)
	select {
	case <-stopped:
		t.Fatal("stop returned before the admitted renewal finished")
	default:
	}
	finishOnce.Do(func() { close(finish) })
	waitLeaseSignal(t, stopped)
	stop()
}

func TestLeaseRenewalWithCanceledParentDoesNotSubmit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler := &countingLeaseScheduler{}
	var renewals atomic.Int32
	stop := worker.StartLeaseRenewal(ctx, scheduler, time.Hour, func(context.Context) {
		renewals.Add(1)
	}, nil)
	stop()
	if got := scheduler.calls.Load(); got != 0 {
		t.Fatalf("scheduler calls = %d, want 0", got)
	}
	if got := renewals.Load(); got != 0 {
		t.Fatalf("renewal calls = %d, want 0", got)
	}
}

func TestLeaseRenewalReportsPermanentSubmitErrorOnce(t *testing.T) {
	wantErr := errors.New("scheduler stopped")
	scheduler := &failingLeaseScheduler{err: wantErr}
	errorsSeen := make(chan error, 2)
	var renewals atomic.Int32
	stop := worker.StartLeaseRenewal(context.Background(), scheduler, 5*time.Millisecond, func(context.Context) {
		renewals.Add(1)
	}, func(err error) { errorsSeen <- err })
	waitLeaseError(t, errorsSeen, wantErr)
	stop()
	if got := scheduler.calls.Load(); got != 1 {
		t.Fatalf("scheduler calls = %d, want 1", got)
	}
	if got := renewals.Load(); got != 0 {
		t.Fatalf("renewal calls = %d, want 0", got)
	}
	select {
	case err := <-errorsSeen:
		t.Fatalf("onError called more than once: %v", err)
	default:
	}
}

func newLeaseTestPools(t *testing.T) *worker.Pools {
	t.Helper()
	pools := worker.NewPoolsConfig(0, 1, slog.New(slog.DiscardHandler))
	t.Cleanup(pools.Close)
	return pools
}

type observedLeaseScheduler struct {
	pool       *ants.Pool
	overloaded chan struct{}
}

func (s *observedLeaseScheduler) Submit(task func()) error {
	err := s.pool.Submit(task)
	if errors.Is(err, ants.ErrPoolOverload) {
		select {
		case s.overloaded <- struct{}{}:
		default:
		}
	}
	if err != nil {
		return fmt.Errorf("submit observed lease renewal: %w", err)
	}
	return nil
}

type inlineLeaseScheduler struct{}

func (inlineLeaseScheduler) Submit(task func()) error { go task(); return nil }

type countingLeaseScheduler struct{ calls atomic.Int32 }

func (s *countingLeaseScheduler) Submit(task func()) error {
	s.calls.Add(1)
	go task()
	return nil
}

type failingLeaseScheduler struct {
	err   error
	calls atomic.Int32
}

func (s *failingLeaseScheduler) Submit(func()) error { s.calls.Add(1); return s.err }

func waitLeaseSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lease renewal event")
	}
}

func waitLeaseError(t *testing.T, got <-chan error, want error) {
	t.Helper()
	select {
	case err := <-got:
		if !errors.Is(err, want) {
			t.Fatalf("onError = %v, want %v", err, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for onError")
	}
}

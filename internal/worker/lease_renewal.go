package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"
)

// LeaseScheduler accepts short renewal tasks without blocking when it is full.
// An ants pool must use WithNonblocking(true); saturation returns ErrPoolOverload.
type LeaseScheduler interface {
	Submit(func()) error
}

// StartLeaseRenewal keeps timer waits outside the worker pool. Each active lease
// owns one cancellable timer goroutine; only its short renewal uses a worker.
// The returned idempotent stop waits for any admitted renewal before returning,
// so callers can safely release the lease afterwards.
func StartLeaseRenewal(
	ctx context.Context,
	scheduler LeaseScheduler,
	interval time.Duration,
	renew func(context.Context),
	onError func(error),
) func() {
	if scheduler == nil || interval <= 0 {
		return func() {}
	}
	// Disabled worker pools return a typed nil through the scheduler interface.
	if pool, ok := scheduler.(*ants.Pool); ok && pool == nil {
		return func() {}
	}
	renewCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runLeaseRenewal(renewCtx, scheduler, interval, renew, onError)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

func runLeaseRenewal(ctx context.Context, scheduler LeaseScheduler, interval time.Duration, renew func(context.Context), onError func(error)) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !submitLeaseRenewal(ctx, scheduler, min(interval, 100*time.Millisecond), renew, onError) {
				return
			}
			timer.Reset(interval)
		}
	}
}

func submitLeaseRenewal(ctx context.Context, scheduler LeaseScheduler, retryInterval time.Duration, renew func(context.Context), onError func(error)) bool {
	done := make(chan struct{})
	for ctx.Err() == nil {
		err := scheduler.Submit(leaseRenewalTask(ctx, renew, done))
		if err == nil {
			<-done
			return ctx.Err() == nil
		}
		if !errors.Is(err, ants.ErrPoolOverload) {
			if onError != nil {
				onError(err)
			}
			return false
		}
		if !waitForLeaseWorker(ctx, retryInterval) {
			return false
		}
	}
	return false
}

func leaseRenewalTask(ctx context.Context, renew func(context.Context), done chan<- struct{}) func() {
	return func() {
		defer close(done)
		if ctx.Err() == nil {
			renew(ctx)
		}
	}
}

func waitForLeaseWorker(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

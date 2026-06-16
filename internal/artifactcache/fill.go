package artifactcache

import (
	"context"
	"log/slog"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
)

const (
	defaultFillLeaseTTL     = 5 * time.Minute
	defaultFillPollInterval = 100 * time.Millisecond
	fillLeaseReleaseTimeout = 2 * time.Second
)

type FillTracker struct {
	fills          collectionmapping.ConcurrentMap[string, *Fill]
	locker         FillLocker
	leaseScheduler FillLeaseScheduler
	leaseTTL       time.Duration
	pollInterval   time.Duration
	logger         *slog.Logger
}

type Fill struct {
	done chan struct{}
	err  error
}

type FillLease interface {
	Release(ctx context.Context) error
	Extend(ctx context.Context, ttl time.Duration) error
}

type FillLocker interface {
	AcquireLease(ctx context.Context, key string, ttl time.Duration) (FillLease, bool, error)
}

type FillLeaseScheduler interface {
	Submit(func()) error
}

type FillTrackerOption func(*FillTracker)

type FillWaitFunc[T any] func() (T, bool, error)

type FillOwnerFunc[T any] func() (T, error)

type CoalesceRequest[T any] struct {
	Context context.Context
	Tracker *FillTracker
	Key     Key
	Wait    FillWaitFunc[T]
	Fill    FillOwnerFunc[T]
}

func NewFillTracker(options ...FillTrackerOption) *FillTracker {
	tracker := &FillTracker{
		leaseTTL:     defaultFillLeaseTTL,
		pollInterval: defaultFillPollInterval,
	}
	for _, option := range options {
		if option != nil {
			option(tracker)
		}
	}
	if tracker.leaseTTL <= 0 {
		tracker.leaseTTL = defaultFillLeaseTTL
	}
	if tracker.pollInterval <= 0 {
		tracker.pollInterval = defaultFillPollInterval
	}
	return tracker
}

func WithFillLocker(locker FillLocker) FillTrackerOption {
	return func(t *FillTracker) {
		t.locker = locker
	}
}

func WithFillLeaseScheduler(scheduler FillLeaseScheduler) FillTrackerOption {
	return func(t *FillTracker) {
		t.leaseScheduler = scheduler
	}
}

func WithFillLeaseTTL(ttl time.Duration) FillTrackerOption {
	return func(t *FillTracker) {
		t.leaseTTL = ttl
	}
}

func WithFillPollInterval(interval time.Duration) FillTrackerOption {
	return func(t *FillTracker) {
		t.pollInterval = interval
	}
}

func WithFillLogger(logger *slog.Logger) FillTrackerOption {
	return func(t *FillTracker) {
		t.logger = logger
	}
}

func (t *FillTracker) Begin(key Key) (*Fill, bool) {
	cacheKey := fillKey(key)
	if t == nil || cacheKey == "" {
		return nil, true
	}
	fill := &Fill{done: make(chan struct{})}
	actual, loaded := t.fills.GetOrStore(cacheKey, fill)
	return actual, !loaded
}

func (t *FillTracker) Finish(key Key, fill *Fill, err error) {
	if t == nil || fill == nil {
		return
	}
	cacheKey := fillKey(key)
	t.fills.LoadAndDelete(cacheKey)
	fill.err = err
	close(fill.done)
}

func (f *Fill) Wait(ctx context.Context) error {
	if f == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return wrapError(ctx.Err(), "wait for artifact cache fill")
	case <-f.done:
		return f.err
	}
}

func CoalesceFill[T any](
	ctx context.Context,
	tracker *FillTracker,
	key Key,
	wait FillWaitFunc[T],
	fill FillOwnerFunc[T],
) (T, error) {
	return CoalesceFillWith(CoalesceRequest[T]{
		Context: ctx,
		Tracker: tracker,
		Key:     key,
		Wait:    wait,
		Fill:    fill,
	})
}

//nolint:gocognit // The owner/waiter retry loop is clearer kept as one generic coordination state machine.
func CoalesceFillWith[T any](req CoalesceRequest[T]) (T, error) {
	var zero T
	for {
		current, owner := req.Tracker.Begin(req.Key)
		if !owner {
			if err := current.Wait(req.Context); err != nil && req.Context.Err() != nil {
				return zero, err
			}
			result, ok, err := req.Wait()
			if ok || err != nil {
				return result, err
			}
			continue
		}

		result, err := coalesceFillOwner(req)
		req.Tracker.Finish(req.Key, current, err)
		return result, err
	}
}

//nolint:gocognit // Lease ownership and waiter polling are one small state machine with explicit retry branches.
func coalesceFillOwner[T any](req CoalesceRequest[T]) (T, error) {
	var zero T
	for {
		result, ok, err := req.Wait()
		if ok || err != nil {
			return result, err
		}

		lease, owner := acquireFillLease(req.Context, req.Tracker, req.Key)
		if owner {
			if lease == nil {
				return req.Fill()
			}
			return fillWithLease(req.Context, req.Tracker, lease, req.Fill)
		}
		if err := waitForFillPoll(req.Context, req.Tracker); err != nil {
			return zero, err
		}
	}
}

func fillKey(key Key) string {
	if key.Alias == "" || key.Repository == "" || key.Reference == "" {
		return ""
	}
	return key.Alias + "\x00" + key.Repository + "\x00" + key.Reference
}

package artifactcache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/coalescer"
)

const (
	defaultFillLeaseTTL     = 5 * time.Minute
	defaultFillPollInterval = 100 * time.Millisecond
	fillLeaseReleaseTimeout = 2 * time.Second
)

type FillTracker struct {
	fills          *coalescer.Tracker
	locker         FillLocker
	leaseScheduler FillLeaseScheduler
	leaseTTL       time.Duration
	pollInterval   time.Duration
	logger         *slog.Logger
}

type Fill = coalescer.Fill

// FillLease reuses the backend lease type so provider method signatures match.
type FillLease = backend.Lease

// FillLocker is the optional distributed lease capability of a cache backend.
type FillLocker = backend.LeaseBackend

type FillLeaseScheduler interface {
	Submit(func()) error
}

type FillTrackerOption func(*FillTracker)

type FillWaitFunc[T any] func() (T, bool, error)

type FillOwnerFunc[T any] func() (T, error)

type CoalesceRequest[T any] struct {
	Context context.Context
	Key     Key
	Wait    FillWaitFunc[T]
	Fill    FillOwnerFunc[T]
}

func NewFillTracker(options ...FillTrackerOption) *FillTracker {
	tracker := &FillTracker{
		fills:        coalescer.NewTracker(),
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
	return t.fills.Begin(cacheKey)
}

func (t *FillTracker) Finish(key Key, fill *Fill, err error) {
	if t == nil || fill == nil {
		return
	}
	cacheKey := fillKey(key)
	t.fills.Finish(cacheKey, fill, err)
}

func (t *FillTracker) Coalesce[T any](req CoalesceRequest[T]) (T, error) {
	for {
		current, owner := t.Begin(req.Key)
		if owner {
			return t.finishTrackedFill(req, current)
		}
		result, retry, err := t.waitForTrackedFill(req, current)
		if !retry {
			return result, err
		}
	}
}

func (t *FillTracker) waitForTrackedFill[T any](req CoalesceRequest[T], current *Fill) (T, bool, error) {
	var zero T
	if err := current.Wait(req.Context); err != nil {
		if req.Context != nil && req.Context.Err() != nil {
			return zero, false, wrapError(req.Context.Err(), "wait for artifact cache fill")
		}
		if canceled, ok := errors.AsType[*fillOwnerCanceledError](err); ok && canceled != nil {
			return zero, true, nil //nolint:nilerr // Healthy followers retry when only the fill owner was canceled.
		}
		return zero, false, wrapError(err, "wait for artifact cache fill")
	}
	result, ok, err := req.Wait()
	return result, !ok && err == nil, err
}

func (t *FillTracker) finishTrackedFill[T any](req CoalesceRequest[T], current *Fill) (T, error) {
	result, err := t.coalesceFillOwner(req)
	trackedErr := err
	if err != nil && req.Context != nil && req.Context.Err() != nil {
		trackedErr = &fillOwnerCanceledError{cause: err}
	}
	t.Finish(req.Key, current, trackedErr)
	return result, err
}

// Only cancellation of the owner's request permits another caller to take over.
// Backend timeouts with a live owner still propagate instead of causing a retry wave.
type fillOwnerCanceledError struct {
	cause error
}

func (e *fillOwnerCanceledError) Error() string { return e.cause.Error() }

func (e *fillOwnerCanceledError) Unwrap() error { return e.cause }

func (t *FillTracker) coalesceFillOwner[T any](req CoalesceRequest[T]) (T, error) {
	var zero T
	for {
		result, ok, err := req.Wait()
		if ok || err != nil {
			return result, err
		}
		result, filled, err := t.fillAsLeaseOwner(req)
		if filled || err != nil {
			return result, err
		}
		if err := waitForFillPoll(req.Context, t); err != nil {
			return zero, err
		}
	}
}

func (t *FillTracker) fillAsLeaseOwner[T any](req CoalesceRequest[T]) (T, bool, error) {
	var zero T
	lease, owner := acquireFillLease(req.Context, t, req.Key)
	if !owner {
		return zero, false, nil
	}
	if lease == nil {
		result, err := req.Fill()
		return result, true, err
	}
	result, err := t.fillWithLease(req.Context, lease, req.Fill)
	return result, true, err
}

func fillKey(key Key) string {
	if key.Alias == "" || key.Repository == "" || key.Reference == "" {
		return ""
	}
	return key.Alias + "\x00" + key.Repository + "\x00" + key.Reference
}

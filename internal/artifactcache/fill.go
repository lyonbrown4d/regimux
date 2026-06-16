package artifactcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	fills        collectionmapping.ConcurrentMap[string, *Fill]
	locker       FillLocker
	leaseTTL     time.Duration
	pollInterval time.Duration
	logger       *slog.Logger
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

type FillTrackerOption func(*FillTracker)

type FillWaitFunc[T any] func() (T, bool, error)

type FillOwnerFunc[T any] func() (T, error)

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
	if ctx == nil {
		ctx = context.Background()
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
	var zero T
	for {
		current, owner := tracker.Begin(key)
		if !owner {
			if err := current.Wait(ctx); err != nil && ctx.Err() != nil {
				return zero, err
			}
			result, ok, err := wait()
			if ok || err != nil {
				return result, err
			}
			continue
		}

		result, err := coalesceFillOwner(ctx, tracker, key, wait, fill)
		tracker.Finish(key, current, err)
		return result, err
	}
}

func coalesceFillOwner[T any](
	ctx context.Context,
	tracker *FillTracker,
	key Key,
	wait FillWaitFunc[T],
	fill FillOwnerFunc[T],
) (T, error) {
	var zero T
	for {
		result, ok, err := wait()
		if ok || err != nil {
			return result, err
		}

		lease, owner := acquireFillLease(ctx, tracker, key)
		if owner {
			if lease == nil {
				return fill()
			}
			return fillWithLease(ctx, tracker, lease, fill)
		}
		if err := waitForFillPoll(ctx, tracker); err != nil {
			return zero, err
		}
	}
}

func acquireFillLease(ctx context.Context, tracker *FillTracker, key Key) (FillLease, bool) {
	if tracker == nil || tracker.locker == nil {
		return nil, true
	}
	leaseKey := distributedFillKey(key)
	if leaseKey == "" {
		return nil, true
	}
	lease, ok, err := tracker.locker.AcquireLease(ctx, leaseKey, tracker.leaseTTL)
	if err != nil {
		if tracker.logger != nil {
			tracker.logger.WarnContext(ctx, "artifact cache fill lease unavailable; falling back to local fill",
				"alias", key.Alias,
				"repository", key.Repository,
				"reference", key.Reference,
				"error", err,
			)
		}
		return nil, true
	}
	return lease, ok
}

func fillWithLease[T any](ctx context.Context, tracker *FillTracker, lease FillLease, fill FillOwnerFunc[T]) (T, error) {
	stopRenew := renewFillLease(ctx, tracker, lease)
	result, err := fill()
	close(stopRenew)
	releaseFillLease(lease)
	return result, err
}

func renewFillLease(ctx context.Context, tracker *FillTracker, lease FillLease) chan struct{} {
	stop := make(chan struct{})
	if tracker == nil || lease == nil || tracker.leaseTTL <= 0 {
		return stop
	}
	interval := tracker.leaseTTL / 2
	if interval <= 0 {
		return stop
	}
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-timer.C:
				extendCtx, cancel := context.WithTimeout(context.Background(), fillLeaseReleaseTimeout)
				err := lease.Extend(extendCtx, tracker.leaseTTL)
				cancel()
				if err != nil && tracker.logger != nil {
					tracker.logger.WarnContext(ctx, "artifact cache fill lease extend failed", "error", err)
				}
				timer.Reset(interval)
			}
		}
	}()
	return stop
}

func releaseFillLease(lease FillLease) {
	if lease == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fillLeaseReleaseTimeout)
	defer cancel()
	_ = lease.Release(ctx)
}

func waitForFillPoll(ctx context.Context, tracker *FillTracker) error {
	if ctx == nil {
		ctx = context.Background()
	}
	interval := defaultFillPollInterval
	if tracker != nil && tracker.pollInterval > 0 {
		interval = tracker.pollInterval
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return wrapError(ctx.Err(), "wait for artifact cache fill lease")
	case <-timer.C:
		return nil
	}
}

func fillKey(key Key) string {
	if key.Alias == "" || key.Repository == "" || key.Reference == "" {
		return ""
	}
	return key.Alias + "\x00" + key.Repository + "\x00" + key.Reference
}

func distributedFillKey(key Key) string {
	raw := fillKey(key)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return "artifact-cache:fill:" + hex.EncodeToString(sum[:])
}

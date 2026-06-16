package artifactcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

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
	stopRenew := startFillLeaseRenewal(ctx, tracker, lease)
	result, err := fill()
	stopRenew()
	releaseFillLease(ctx, tracker, lease)
	return result, err
}

func startFillLeaseRenewal(ctx context.Context, tracker *FillTracker, lease FillLease) func() {
	stop := make(chan struct{})
	interval, ok := fillLeaseRenewInterval(tracker, lease)
	if !ok {
		return func() { close(stop) }
	}
	if tracker == nil || tracker.leaseScheduler == nil {
		logFillLeaseSchedulerUnavailable(ctx, tracker)
		return func() { close(stop) }
	}
	if err := tracker.leaseScheduler.Submit(func() {
		runFillLeaseRenewer(ctx, tracker, lease, interval, stop)
	}); err != nil {
		if tracker.logger != nil {
			tracker.logger.WarnContext(ctx, "artifact cache fill lease renewal scheduling failed", "error", err)
		}
		return func() { close(stop) }
	}
	return func() { close(stop) }
}

func logFillLeaseSchedulerUnavailable(ctx context.Context, tracker *FillTracker) {
	if tracker != nil && tracker.logger != nil {
		tracker.logger.WarnContext(ctx, "artifact cache fill lease renewal scheduler unavailable")
	}
}

func fillLeaseRenewInterval(tracker *FillTracker, lease FillLease) (time.Duration, bool) {
	if tracker == nil || lease == nil || tracker.leaseTTL <= 0 {
		return 0, false
	}
	interval := tracker.leaseTTL / 2
	return interval, interval > 0
}

func runFillLeaseRenewer(ctx context.Context, tracker *FillTracker, lease FillLease, interval time.Duration, stop <-chan struct{}) {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			extendFillLease(ctx, tracker, lease)
			timer.Reset(interval)
		}
	}
}

func extendFillLease(ctx context.Context, tracker *FillTracker, lease FillLease) {
	extendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fillLeaseReleaseTimeout)
	defer cancel()
	if err := lease.Extend(extendCtx, tracker.leaseTTL); err != nil && tracker.logger != nil {
		tracker.logger.WarnContext(ctx, "artifact cache fill lease extend failed", "error", err)
	}
}

func releaseFillLease(ctx context.Context, tracker *FillTracker, lease FillLease) {
	if lease == nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fillLeaseReleaseTimeout)
	defer cancel()
	if err := lease.Release(releaseCtx); err != nil && tracker != nil && tracker.logger != nil {
		tracker.logger.WarnContext(ctx, "artifact cache fill lease release failed", "error", err)
	}
}

func waitForFillPoll(ctx context.Context, tracker *FillTracker) error {
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

func distributedFillKey(key Key) string {
	raw := fillKey(key)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return "artifact-cache:fill:" + hex.EncodeToString(sum[:])
}

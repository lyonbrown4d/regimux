package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

const (
	blobFillLeaseTTL            = 5 * time.Minute
	blobFillLeasePollInterval   = 100 * time.Millisecond
	blobFillLeaseReleaseTimeout = 2 * time.Second
)

func (p blobProxy) acquireBlobFillLease(ctx context.Context, req BlobRequest) (backend.Lease, bool) {
	locker, ok := p.cache.(backend.LeaseBackend)
	if !ok || locker == nil {
		return nil, true
	}
	key := blobFillLeaseKey(req)
	if key == "" {
		return nil, true
	}
	lease, acquired, err := locker.AcquireLease(ctx, key, blobFillLeaseTTL)
	if err != nil {
		p.logBlobStreamCacheError(ctx, req, "blob fill lease unavailable; falling back to local fill", err)
		return nil, true
	}
	return lease, acquired
}

func (p blobProxy) startBlobFillLease(ctx context.Context, req BlobRequest, lease backend.Lease) func() {
	if lease == nil {
		return func() {}
	}
	stop := make(chan struct{})
	var once sync.Once
	p.scheduleBlobFillLeaseRenewal(ctx, req, lease, stop)
	return func() {
		once.Do(func() {
			close(stop)
			p.releaseBlobFillLease(ctx, req, lease)
		})
	}
}

func (p blobProxy) scheduleBlobFillLeaseRenewal(ctx context.Context, req BlobRequest, lease backend.Lease, stop <-chan struct{}) {
	if p.leaseScheduler == nil {
		p.logBlobStreamCacheError(
			ctx,
			req,
			"blob fill lease renewal scheduler unavailable",
			errors.New("lease renewal scheduler unavailable"),
		)
		return
	}
	if err := p.leaseScheduler.Submit(func() {
		p.renewBlobFillLease(ctx, req, lease, stop)
	}); err != nil {
		p.logBlobStreamCacheError(ctx, req, "submit blob fill lease renewal failed", err)
	}
}

func (p blobProxy) releaseBlobFillLease(ctx context.Context, req BlobRequest, lease backend.Lease) {
	if lease == nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), blobFillLeaseReleaseTimeout)
	defer cancel()
	if err := lease.Release(releaseCtx); err != nil {
		p.logBlobStreamCacheError(ctx, req, "release blob fill lease failed", err)
	}
}

func (p blobProxy) renewBlobFillLease(ctx context.Context, req BlobRequest, lease backend.Lease, stop <-chan struct{}) {
	interval := blobFillLeaseTTL / 2
	if interval <= 0 {
		return
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			extendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), blobFillLeaseReleaseTimeout)
			err := lease.Extend(extendCtx, blobFillLeaseTTL)
			cancel()
			if err != nil {
				p.logBlobStreamCacheError(ctx, req, "extend blob fill lease failed", err)
			}
			timer.Reset(interval)
		}
	}
}

func waitForBlobFillLeasePoll(ctx context.Context) error {
	timer := time.NewTimer(blobFillLeasePollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return wrapError(ctx.Err(), "wait for distributed blob fill")
	case <-timer.C:
		return nil
	}
}

func blobFillLeaseKey(req BlobRequest) string {
	if req.Digest == "" {
		return ""
	}
	return "container:blob:fill:" + req.Digest
}

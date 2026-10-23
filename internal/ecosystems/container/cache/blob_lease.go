package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/worker"
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
	var once sync.Once
	stop := p.scheduleBlobFillLeaseRenewal(ctx, req, lease)
	return func() {
		once.Do(func() {
			stop()
			p.releaseBlobFillLease(ctx, req, lease)
		})
	}
}

func (p blobProxy) scheduleBlobFillLeaseRenewal(ctx context.Context, req BlobRequest, lease backend.Lease) func() {
	if p.leaseScheduler == nil {
		p.logBlobStreamCacheError(
			ctx,
			req,
			"blob fill lease renewal scheduler unavailable",
			errors.New("lease renewal scheduler unavailable"),
		)
		return func() {}
	}
	return worker.StartLeaseRenewal(ctx, p.leaseScheduler, blobFillLeaseTTL/2, func(renewCtx context.Context) {
		p.renewBlobFillLease(renewCtx, req, lease)
	}, func(err error) {
		p.logBlobStreamCacheError(ctx, req, "submit blob fill lease renewal failed", err)
	})
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

func (p blobProxy) renewBlobFillLease(ctx context.Context, req BlobRequest, lease backend.Lease) {
	extendCtx, cancel := context.WithTimeout(ctx, blobFillLeaseReleaseTimeout)
	defer cancel()
	if err := lease.Extend(extendCtx, blobFillLeaseTTL); err != nil {
		p.logBlobStreamCacheError(ctx, req, "extend blob fill lease failed", err)
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

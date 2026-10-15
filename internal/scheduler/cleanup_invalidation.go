package scheduler

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) invalidatePackageCache(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
	var invalidateErr error
	r.Capabilities[ecosystem.CacheCleanupInvalidator]().Range(func(_ int, invalidator ecosystem.CacheCleanupInvalidator) bool {
		if group.Ecosystem != "" && invalidator.Name() != group.Ecosystem {
			return true
		}
		target := group
		target.Ecosystem = invalidator.Name()
		if err := invalidator.InvalidateCacheCleanup(ctx, target); err != nil {
			invalidateErr = oops.With("ecosystem", invalidator.Name()).Wrapf(err, "invalidate package cache before retirement")
			return false
		}
		return true
	})
	return invalidateErr
}

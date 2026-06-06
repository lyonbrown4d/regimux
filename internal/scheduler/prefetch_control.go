package scheduler

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) CancelPrefetch(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
	controller := r.prefetchController()
	if controller == nil {
		return nil, oops.In("scheduler").Errorf("prefetch control service is not configured")
	}
	report, err := controller.CancelPrefetch(ctx)
	if err != nil {
		return nil, oops.Wrapf(err, "cancel prefetch")
	}
	return report, nil
}

func (r *Runtime) RetryPrefetch(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
	controller := r.prefetchController()
	if controller == nil {
		return nil, oops.In("scheduler").Errorf("prefetch control service is not configured")
	}
	report, err := controller.RetryPrefetch(ctx)
	if err != nil {
		return nil, oops.Wrapf(err, "retry prefetch")
	}
	return report, nil
}

func (r *Runtime) prefetchController() ecosystem.PrefetchController {
	if r == nil || r.runtimes == nil {
		return nil
	}
	var match ecosystem.PrefetchController
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		if runtime == nil {
			return true
		}
		controller, ok := runtime.(ecosystem.PrefetchController)
		if !ok {
			return true
		}
		match = controller
		return false
	})
	return match
}

var _ ecosystem.PrefetchController = (*Runtime)(nil)

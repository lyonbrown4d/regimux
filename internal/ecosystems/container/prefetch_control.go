package container

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) CancelPrefetch(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
	if r == nil || r.prefetch == nil {
		return nil, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container prefetch service is not configured")
	}
	report, err := r.prefetch.CancelPrefetch(ctx)
	if err != nil {
		return nil, oops.Wrapf(err, "cancel container prefetch")
	}
	return report, nil
}

func (r *Runtime) RetryPrefetch(ctx context.Context) (*ecosystem.PrefetchControlReport, error) {
	if r == nil || r.prefetch == nil {
		return nil, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container prefetch service is not configured")
	}
	report, err := r.prefetch.RetryPrefetch(ctx)
	if err != nil {
		return nil, oops.Wrapf(err, "retry container prefetch")
	}
	return report, nil
}

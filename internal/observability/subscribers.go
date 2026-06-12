package observability

import (
	"context"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/events"
)

func NewUpstreamMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(ctx context.Context, event events.UpstreamRequest) error {
		metrics.ObserveUpstreamRequest(ctx, UpstreamRequestMetric{
			Alias:     event.Alias,
			Operation: event.Operation,
			Method:    event.Method,
			Registry:  event.Registry,
			Status:    event.Status,
			Attempts:  event.Attempts,
			Duration:  event.Duration,
			Size:      event.Size,
			Err:       errorFromMessage(event.Error),
		})
		return nil
	})
}

func NewFailoverMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(ctx context.Context, event events.UpstreamFailover) error {
		metrics.ObserveUpstreamFailover(ctx, event.Alias, event.Operation, event.Registry, event.HasNext, errorFromMessage(event.Error))
		return nil
	})
}

func NewCacheAccessMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(ctx context.Context, event events.CacheAccess) error {
		metrics.ObserveCacheAccess(ctx, event.Kind, event.Alias, event.Repository, event.Status)
		return nil
	})
}

func NewCacheStoreMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(ctx context.Context, event events.CacheStore) error {
		metrics.ObserveCacheStore(ctx, event.Kind, event.Alias, event.Repository, event.Size)
		return nil
	})
}

func NewDependencyPulledMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(ctx context.Context, event events.DependencyPulled) error {
		metrics.ObserveDependencyPull(ctx, DependencyPullMetric{
			Ecosystem:  event.Ecosystem,
			Kind:       event.Kind,
			Alias:      event.Alias,
			Repository: event.Repository,
			Status:     event.Status,
		})
		return nil
	})
}

func NewDependencyPullDeniedMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(ctx context.Context, event events.DependencyPullDenied) error {
		metrics.ObserveDependencyPolicyDeniedPull(ctx, DependencyPolicyDeniedPullMetric{
			Ecosystem:  event.Ecosystem,
			Kind:       event.Kind,
			Alias:      event.Alias,
			Repository: event.Repository,
		})
		return nil
	})
}

func errorFromMessage(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	return metricEventError(message)
}

type metricEventError string

func (e metricEventError) Error() string {
	return string(e)
}

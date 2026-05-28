package observability

import (
	"context"
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/logx"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/samber/oops"
)

var Module = dix.NewModule("observability",
	dix.Providers(
		dix.Provider1[config.LogConfig, config.Config](func(cfg config.Config) config.LogConfig {
			return cfg.Log
		}),
		dix.ProviderErr1[*slog.Logger, config.LogConfig](NewLogger),
		dix.Provider1[*Metrics, *slog.Logger](NewMetrics, dix.Eager()),
		dix.Contribute1[events.Subscriber, *Metrics](
			NewUpstreamMetricsSubscriber,
			dix.Key("metrics.upstream"), dix.Order(10),
		),
		dix.Contribute1[events.Subscriber, *Metrics](
			NewFailoverMetricsSubscriber,
			dix.Key("metrics.failover"), dix.Order(11),
		),
		dix.Contribute1[events.Subscriber, *Metrics](
			NewCacheAccessMetricsSubscriber,
			dix.Key("metrics.cache_access"), dix.Order(12),
		),
		dix.Contribute1[events.Subscriber, *Metrics](
			NewCacheStoreMetricsSubscriber,
			dix.Key("metrics.cache_store"), dix.Order(13),
		),
	),
	dix.Hooks(
		dix.OnStop[*slog.Logger](
			closeLogger,
			dix.LifecycleName("regimux.logger_close"),
			dix.LifecyclePriority(-240),
		),
	),
)

func closeLogger(_ context.Context, logger *slog.Logger) error {
	if logger == nil {
		return nil
	}
	logger.Info("closing logger")
	if err := logx.Close(logger); err != nil {
		return oops.Wrapf(err, "close logger")
	}
	return nil
}

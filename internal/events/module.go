package events

import (
	"context"
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/oops"
)

var Module = dix.NewModule("events",
	dix.Providers(
		dix.Provider1[Bus, *slog.Logger](NewBus, dix.Eager()),
		dix.Provider0[*Subscriptions](NewSubscriptions, dix.Eager()),
		dix.Provider1[Subscriber, *slog.Logger](
			NewLifecycleLogSubscriber,
			dix.Into[Subscriber](dix.Key("events.lifecycle_logger"), dix.Order(-100)),
		),
	),
	dix.Hooks(
		dix.OnStart3[config.Config, *slog.Logger, build.Version](logStartup, dix.LifecycleName("regimux.log_startup"), dix.LifecyclePriority(-200)),
		dix.OnStart2[Bus, build.Version](publishStarting, dix.LifecycleName("regimux.application_starting"), dix.LifecyclePriority(-100)),
		dix.OnStart3[Bus, *Subscriptions, *collectionlist.List[Subscriber]](
			startSubscribers,
			dix.LifecycleName("events.subscribers_start"),
			dix.LifecyclePriority(-150),
		),
		dix.OnStart2[Bus, build.Version](publishStarted, dix.LifecycleName("regimux.application_started"), dix.LifecyclePriority(100)),
		dix.OnStop2[Bus, build.Version](publishStopping, dix.LifecycleName("regimux.application_stopping"), dix.LifecyclePriority(100)),
		dix.OnStop[*Subscriptions](
			stopSubscribers,
			dix.LifecycleName("events.subscribers_stop"),
			dix.LifecyclePriority(-190),
		),
		dix.OnStop2[Bus, build.Version](
			publishStopped,
			dix.LifecycleName("regimux.application_stopped"),
			dix.LifecyclePriority(-100),
		),
		dix.OnStop[Bus](closeBus, dix.LifecycleName("regimux.events_close"), dix.LifecyclePriority(-200)),
	),
)

func startSubscribers(_ context.Context, bus Bus, subscriptions *Subscriptions, subscribers *collectionlist.List[Subscriber]) error {
	return subscriptions.Register(bus, subscribers)
}

func stopSubscribers(_ context.Context, subscriptions *Subscriptions) error {
	return subscriptions.Close()
}

func logStartup(_ context.Context, cfg config.Config, logger *slog.Logger, version build.Version) error {
	ordered := cfg.OrderedUpstreams()
	logger.Info("regimuxd starting",
		"version", string(version),
		"listen", cfg.Server.Listen,
		"upstream_count", ordered.Len(),
		"upstreams", ordered.Keys(),
	)
	return nil
}

func publishStarting(ctx context.Context, bus Bus, version build.Version) error {
	return Publish(ctx, bus, ApplicationStarting{Version: string(version)})
}

func publishStarted(ctx context.Context, bus Bus, version build.Version) error {
	return Publish(ctx, bus, ApplicationStarted{Version: string(version)})
}

func publishStopping(ctx context.Context, bus Bus, version build.Version) error {
	return Publish(ctx, bus, ApplicationStopping{Version: string(version)})
}

func publishStopped(ctx context.Context, bus Bus, version build.Version) error {
	return Publish(ctx, bus, ApplicationStopped{Version: string(version)})
}

func closeBus(_ context.Context, bus Bus) error {
	if bus == nil {
		return nil
	}
	if err := bus.Close(); err != nil {
		return oops.Wrapf(err, "close event bus")
	}
	return nil
}

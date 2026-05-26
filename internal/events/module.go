package events

import (
	"context"
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
)

func Module(observabilityModule dix.Module) dix.Module {
	return dix.NewModule("events",
		dix.Imports(observabilityModule),
		dix.Providers(
			dix.Provider1[Bus, *slog.Logger](NewBus, dix.Eager()),
			dix.Provider0[*Subscriptions](NewSubscriptions, dix.Eager()),
			dix.Provider1[Subscriber, *slog.Logger](
				NewLifecycleLogSubscriber,
				dix.Into[Subscriber](dix.Key("events.lifecycle_logger"), dix.Order(-100)),
			),
		),
		dix.Hooks(
			dix.OnStart3[Bus, *Subscriptions, *collectionlist.List[Subscriber]](
				startSubscribers,
				dix.LifecycleName("events.subscribers_start"),
				dix.LifecyclePriority(-150),
			),
			dix.OnStop[*Subscriptions](
				stopSubscribers,
				dix.LifecycleName("events.subscribers_stop"),
				dix.LifecyclePriority(-190),
			),
		),
	)
}

func startSubscribers(_ context.Context, bus Bus, subscriptions *Subscriptions, subscribers *collectionlist.List[Subscriber]) error {
	return subscriptions.Register(bus, subscribers)
}

func stopSubscribers(_ context.Context, subscriptions *Subscriptions) error {
	return subscriptions.Close()
}

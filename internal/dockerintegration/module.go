package dockerintegration

import (
	"context"
	"log/slog"
	"time"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

var Module = dix.NewModule("docker",
	dix.Providers(
		dix.Provider4[*Service, config.Config, *slog.Logger, events.Bus, *observability.Metrics](
			NewService,
			dix.Eager(),
		),
	),
	dix.Hooks(
		dix.OnStart[*Service](
			startService,
			dix.LifecycleName("regimux.docker_start"),
			dix.LifecyclePriority(20),
			dix.LifecycleTimeout(30*time.Second),
		),
		dix.OnStop[*Service](
			stopService,
			dix.LifecycleName("regimux.docker_stop"),
			dix.LifecyclePriority(10),
			dix.LifecycleTimeout(30*time.Second),
		),
	),
)

func startService(ctx context.Context, service *Service) error {
	if service == nil {
		return nil
	}
	return service.Start(ctx)
}

func stopService(ctx context.Context, service *Service) error {
	if service == nil {
		return nil
	}
	return service.Stop(ctx)
}

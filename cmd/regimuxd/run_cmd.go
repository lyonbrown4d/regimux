package main

import (
	"context"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/configx"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
	storemodule "github.com/lyonbrown4d/regimux/internal/store"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/samber/oops"
	"time"
)

func run(ctx context.Context, configPath string, args ...string) error {
	app := buildApp(configPath, version, args...)
	if err := app.ValidateContext(ctx); err != nil {
		return oops.Wrapf(err, "validate application")
	}
	return app.RunContext(ctx)
}

func buildApp(configPath string, version string, args ...string) *dix.App {
	configModule := config.Module(
		configx.WithFiles(configPath),
		configx.WithArgs(args...),
	)
	observabilityModule := observability.Module
	buildModule := build.Module(version)
	eventsModule := events.Module
	workerModule := worker.Module
	upstreamModule := upstream.Module
	storeModule := storemodule.Module
	cacheModule := cache.Module
	schedulerModule := scheduler.Module
	endpointModule := api.EndpointsModule
	apiModule := api.Module

	return dix.New("regimuxd",
		dix.Version(version),
		dix.AppDescription("RegiMux registry proxy mirror gateway"),
		dix.RunStopTimeout(30*time.Second),
		dix.RecentEvents(128),
		dix.Modules(configModule, buildModule, observabilityModule, eventsModule, workerModule, upstreamModule, storeModule, cacheModule, schedulerModule, endpointModule, apiModule),
	)
}

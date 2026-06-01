package main

import (
	"context"
	"time"

	"github.com/arcgolabs/configx"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/dockerintegration"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/registrytool"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
	storemodule "github.com/lyonbrown4d/regimux/internal/store"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/samber/oops"
)

func run(ctx context.Context, configPath string, args ...string) error {
	app := buildApp(configPath, args...)
	if err := app.ValidateContext(ctx); err != nil {
		return oops.Wrapf(err, "validate application")
	}
	if err := app.RunContext(ctx); err != nil {
		return oops.Wrapf(err, "run application")
	}
	return nil
}

func buildApp(configPath string, args ...string) *dix.App {
	version := build.VersionFromBuildInfo()

	configModule := config.Module(
		configx.WithFiles(configPath),
		configx.WithArgs(args...),
	)
	observabilityModule := observability.Module
	authModule := auth.Module
	buildModule := build.Module
	eventsModule := events.Module
	registryToolModule := registrytool.Module
	workerModule := worker.Module
	upstreamModule := upstream.Module
	storeModule := storemodule.Module
	cacheModule := cache.Module
	schedulerModule := scheduler.Module
	dockerModule := dockerintegration.Module
	adminModule := admin.Module
	endpointModule := api.EndpointsModule
	apiModule := api.Module

	return dix.New("regimuxd",
		dix.Version(version),
		dix.AppDescription("RegiMux registry proxy mirror gateway"),
		dix.RunStopTimeout(30*time.Second),
		dix.RecentEvents(128),
		dix.Modules(configModule, buildModule, observabilityModule, authModule, eventsModule, registryToolModule, workerModule, upstreamModule, storeModule, cacheModule, schedulerModule, adminModule, endpointModule, apiModule, dockerModule),
	)
}

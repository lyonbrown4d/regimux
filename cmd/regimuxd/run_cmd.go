package main

import (
	"context"
	"time"

	"github.com/arcgolabs/configx"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/dockerintegration"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/probehealth"
	"github.com/lyonbrown4d/regimux/internal/registrytool"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
	storemodule "github.com/lyonbrown4d/regimux/internal/store"
	"github.com/lyonbrown4d/regimux/internal/suggestion"
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
	artifactCacheModule := artifactcache.Module
	clientFactoryModule := clientfactory.Module
	observabilityModule := observability.Module
	authModule := auth.Module
	buildModule := build.Module
	eventsModule := events.Module
	registryToolModule := registrytool.Module
	workerModule := worker.Module
	probeHealthModule := probehealth.Module
	upstreamModule := upstream.Module
	storeModule := storemodule.Module
	ecosystemModule := ecosystem.Module
	cacheModule := cache.Module
	suggestionModule := suggestion.Module
	schedulerModule := scheduler.Module
	dockerModule := dockerintegration.Module
	containerModule := container.Module
	goProxyModule := golang.Module
	npmProxyModule := npm.Module
	pypiProxyModule := pypi.Module
	mavenProxyModule := maven.Module
	adminModule := admin.Module
	endpointModule := api.EndpointsModule
	apiModule := api.Module

	return dix.New("regimuxd",
		dix.Version(version),
		dix.AppDescription("RegiMux registry proxy mirror gateway"),
		dix.RunStopTimeout(30*time.Second),
		dix.RecentEvents(128),
		dix.Modules(configModule, buildModule, clientFactoryModule, artifactCacheModule, observabilityModule, authModule, eventsModule, registryToolModule, workerModule, probeHealthModule, upstreamModule, storeModule, ecosystemModule, cacheModule, suggestionModule, schedulerModule, adminModule, containerModule, goProxyModule, npmProxyModule, pypiProxyModule, mavenProxyModule, endpointModule, apiModule, dockerModule),
	)
}

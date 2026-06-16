package main

import (
	"context"
	"time"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/admin"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	cachemodule "github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/probehealth"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
	storemodule "github.com/lyonbrown4d/regimux/internal/store"
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
	configOptions, configOptionsErr := config.BuildLoadOptions(configPath, args...)
	configPathModule := dix.NewModule("config_path",
		dix.Providers(dix.ProviderErr0[configPathValidation](func() (configPathValidation, error) {
			if configOptionsErr != nil {
				return configPathValidation{}, oops.Wrapf(configOptionsErr, "build config load options")
			}
			return configPathValidation{}, nil
		})),
	)
	configModule := config.Module(configOptions...)
	cacheModule := cachemodule.Module
	artifactCacheModule := artifactcache.Module
	clientFactoryModule := clientfactory.Module
	observabilityModule := observability.Module
	authModule := auth.Module
	buildModule := build.Module
	eventsModule := events.Module
	workerModule := worker.Module
	probeHealthModule := probehealth.Module
	storeModule := storemodule.Module
	ecosystemModule := ecosystem.Module
	ecosystemsModule := ecosystems.Module
	schedulerModule := scheduler.Module
	adminModule := admin.Module
	apiModule := api.Module

	return dix.New("regimuxd",
		dix.Version(version),
		dix.AppDescription("RegiMux developer dependency cache gateway"),
		dix.RunStopTimeout(30*time.Second),
		dix.RecentEvents(128),
		dix.Modules(configPathModule, configModule, buildModule, clientFactoryModule, cacheModule, artifactCacheModule, observabilityModule, authModule, eventsModule, workerModule, probeHealthModule, storeModule, ecosystemModule, ecosystemsModule, schedulerModule, adminModule, apiModule),
	)
}

type configPathValidation struct{}

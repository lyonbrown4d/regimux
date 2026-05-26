// Package main runs the RegiMux registry proxy daemon.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/arcgolabs/dix"
	"github.com/arcgolabs/logx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
	storemodule "github.com/lyonbrown4d/regimux/internal/store"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		slog.Error("regimuxd failed", "error", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var configPath string
	var showVersion bool

	cmd := &cobra.Command{
		Use:           "regimuxd",
		Short:         "Run the RegiMux registry proxy mirror gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), version); err != nil {
					return fmt.Errorf("write version: %w", err)
				}
				return nil
			}
			return run(cmd.Context(), configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "configs/regimux.hcl", "path to regimux HCL config file")
	cmd.Flags().BoolVar(&showVersion, "version", false, "print version and exit")
	return cmd
}

func run(ctx context.Context, configPath string) error {
	cfg, err := config.Load(ctx, configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := observability.NewLogger(cfg.Log)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}

	runErr := buildApp(cfg, logger, version).RunContext(ctx)
	closeErr := logx.Close(logger)
	return joinLifecycleErrors(runErr, closeErr)
}

func joinLifecycleErrors(runErr, closeErr error) error {
	switch {
	case runErr != nil && closeErr != nil:
		return errors.Join(
			fmt.Errorf("run application: %w", runErr),
			fmt.Errorf("close logger: %w", closeErr),
		)
	case runErr != nil:
		return fmt.Errorf("run application: %w", runErr)
	case closeErr != nil:
		return fmt.Errorf("close logger: %w", closeErr)
	default:
		return nil
	}
}

func buildApp(cfg config.Config, logger *slog.Logger, version string) *dix.App {
	configModule := dix.NewModule("config",
		dix.Providers(
			dix.Value(cfg),
		),
	)
	observabilityModule := dix.NewModule("observability",
		dix.Providers(
			dix.Value(logger),
		),
	)
	eventsModule := events.Module(observabilityModule)
	upstreamModule := upstream.Module(configModule, observabilityModule)
	storeModule := storemodule.Module(configModule, observabilityModule)
	cacheModule := cache.Module(configModule, observabilityModule, upstreamModule, storeModule)
	endpointModule := api.EndpointsModule(configModule, cacheModule, observabilityModule)
	apiModule := api.Module(configModule, observabilityModule, eventsModule, endpointModule)
	runtimeModule := newRuntimeModule(version, configModule, observabilityModule, eventsModule, apiModule, cacheModule, storeModule)

	return dix.New("regimuxd",
		dix.Version(version),
		dix.AppDescription("RegiMux registry proxy mirror gateway"),
		dix.UseLogger(logger),
		dix.RunStopTimeout(30*time.Second),
		dix.RecentEvents(128),
		dix.Modules(configModule, observabilityModule, eventsModule, upstreamModule, storeModule, cacheModule, endpointModule, apiModule, runtimeModule),
	)
}

func newRuntimeModule(
	version string,
	configModule dix.Module,
	observabilityModule dix.Module,
	eventsModule dix.Module,
	apiModule dix.Module,
	cacheModule dix.Module,
	storeModule dix.Module,
) dix.Module {
	return dix.NewModule("runtime",
		dix.Imports(configModule, observabilityModule, eventsModule, apiModule, cacheModule, storeModule),
		dix.Hooks(
			dix.OnStart2[config.Config, *slog.Logger](logStartup(version), dix.LifecycleName("regimux.log_startup"), dix.LifecyclePriority(-200)),
			dix.OnStart[events.Bus](publishStarting(version), dix.LifecycleName("regimux.application_starting"), dix.LifecyclePriority(-100)),
			dix.OnStart[*api.Server](startServer, dix.LifecycleName("regimux.server_start"), dix.LifecyclePriority(0)),
			dix.OnStart[events.Bus](publishStarted(version), dix.LifecycleName("regimux.application_started"), dix.LifecyclePriority(100)),
			dix.OnStop[events.Bus](publishStopping(version), dix.LifecycleName("regimux.application_stopping"), dix.LifecyclePriority(100)),
			dix.OnStop[*api.Server](stopServer, dix.LifecycleName("regimux.server_stop"), dix.LifecyclePriority(0), dix.LifecycleTimeout(20*time.Second)),
			dix.OnStop[events.Bus](publishStopped(version), dix.LifecycleName("regimux.application_stopped"), dix.LifecyclePriority(-100)),
			dix.OnStop[backend.Backend](closeCacheBackend, dix.LifecycleName("regimux.cache_close"), dix.LifecyclePriority(-150)),
			dix.OnStop[meta.Store](closeMetadataStore, dix.LifecycleName("regimux.meta_store_close"), dix.LifecyclePriority(-160)),
			dix.OnStop[events.Bus](closeBus, dix.LifecycleName("regimux.events_close"), dix.LifecyclePriority(-200)),
		),
	)
}

func logStartup(version string) func(context.Context, config.Config, *slog.Logger) error {
	return func(_ context.Context, cfg config.Config, logger *slog.Logger) error {
		ordered := cfg.OrderedUpstreams()
		logger.Info("regimuxd starting",
			"version", version,
			"listen", cfg.Server.Listen,
			"upstream_count", ordered.Len(),
			"upstreams", ordered.Keys(),
		)
		return nil
	}
}

func publishStarting(version string) func(context.Context, events.Bus) error {
	return func(ctx context.Context, bus events.Bus) error {
		return events.Publish(ctx, bus, events.ApplicationStarting{Version: version})
	}
}

func publishStarted(version string) func(context.Context, events.Bus) error {
	return func(ctx context.Context, bus events.Bus) error {
		return events.Publish(ctx, bus, events.ApplicationStarted{Version: version})
	}
}

func publishStopping(version string) func(context.Context, events.Bus) error {
	return func(ctx context.Context, bus events.Bus) error {
		return events.Publish(ctx, bus, events.ApplicationStopping{Version: version})
	}
}

func publishStopped(version string) func(context.Context, events.Bus) error {
	return func(ctx context.Context, bus events.Bus) error {
		return events.Publish(ctx, bus, events.ApplicationStopped{Version: version})
	}
}

func startServer(ctx context.Context, server *api.Server) error {
	if server == nil {
		return nil
	}
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("start api server: %w", err)
	}
	return nil
}

func stopServer(ctx context.Context, server *api.Server) error {
	if server == nil {
		return nil
	}
	if err := server.Stop(ctx); err != nil {
		return fmt.Errorf("stop api server: %w", err)
	}
	return nil
}

func closeCacheBackend(_ context.Context, cacheBackend backend.Backend) error {
	if cacheBackend == nil {
		return nil
	}
	if err := cacheBackend.Close(); err != nil {
		return fmt.Errorf("close cache backend: %w", err)
	}
	return nil
}

func closeMetadataStore(_ context.Context, store meta.Store) error {
	if store == nil {
		return nil
	}
	if err := store.Close(); err != nil {
		return fmt.Errorf("close metadata store: %w", err)
	}
	return nil
}

func closeBus(_ context.Context, bus events.Bus) error {
	if bus == nil {
		return nil
	}
	if err := bus.Close(); err != nil {
		return fmt.Errorf("close event bus: %w", err)
	}
	return nil
}

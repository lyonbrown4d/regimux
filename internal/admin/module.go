// Package admin exposes the built-in operator UI.
package admin

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

type Dependencies struct {
	Config   config.Config
	Metadata meta.Store
	Upstream *upstream.Client
	Version  build.Version
	Logger   *slog.Logger
	Auth     *auth.Service
	Messages *Messages
	Syncer   ManualSyncer
	Prefetch PrefetchController
}

type baseDependencies struct {
	Config   config.Config
	Metadata meta.Store
	Upstream *upstream.Client
	Version  build.Version
	Logger   *slog.Logger
	Auth     *auth.Service
}

var Module = dix.NewModule("admin",
	dix.Providers(
		dix.Provider6[baseDependencies, config.Config, meta.Store, *upstream.Client, build.Version, *slog.Logger, *auth.Service](newBaseDependencies),
		dix.ProviderErr0[*Messages](NewMessages),
		dix.Provider4[Dependencies, baseDependencies, *scheduler.Runtime, *prefetch.Service, *Messages](newDependencies),
		dix.ProviderErr1[fiber.Views, *Messages](NewTemplateEngine, dix.Into[fiber.Views](dix.Key("admin"), dix.Order(-80))),
		dix.Provider1[*Service, Dependencies](NewService, dix.Into[api.FiberRoute](dix.Key("admin"), dix.Order(-80))),
	),
)

func newBaseDependencies(
	cfg config.Config,
	metadata meta.Store,
	upstreamClient *upstream.Client,
	version build.Version,
	logger *slog.Logger,
	authService *auth.Service,
) baseDependencies {
	return baseDependencies{
		Config:   cfg,
		Metadata: metadata,
		Upstream: upstreamClient,
		Version:  version,
		Logger:   logger,
		Auth:     authService,
	}
}

func newDependencies(base baseDependencies, syncer *scheduler.Runtime, prefetchService *prefetch.Service, messages *Messages) Dependencies {
	return Dependencies{
		Config:   base.Config,
		Metadata: base.Metadata,
		Upstream: base.Upstream,
		Version:  base.Version,
		Logger:   base.Logger,
		Auth:     base.Auth,
		Messages: messages,
		Syncer:   syncer,
		Prefetch: prefetchService,
	}
}

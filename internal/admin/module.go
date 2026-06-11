// Package admin exposes the built-in operator UI.
package admin

import (
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/gofiber/fiber/v3"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/scheduler"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type Dependencies struct {
	Config    config.Config
	Metadata  meta.Store
	Objects   object.Store
	Runtimes  *collectionlist.List[ecosystem.Runtime]
	Version   build.Version
	Logger    *slog.Logger
	Auth      *auth.Service
	Messages  *Messages
	Mapper    *AdminMapper
	Syncer    ManualSyncer
	Prefetch  PrefetchController
	Scheduler SchedulerController
}

type baseDependencies struct {
	Config   config.Config
	Metadata meta.Store
	Objects  object.Store
	Version  build.Version
	Logger   *slog.Logger
	Auth     *auth.Service
}

var Module = dix.NewModule("admin",
	dix.Providers(
		dix.Provider6[baseDependencies, config.Config, meta.Store, object.Store, build.Version, *slog.Logger, *auth.Service](newBaseDependencies),
		dix.Provider0[*AdminMapper](NewAdminMapper),
		dix.ProviderErr0[*Messages](NewMessages),
		dix.Provider5[Dependencies, baseDependencies, *scheduler.Runtime, *collectionlist.List[ecosystem.Runtime], *Messages, *AdminMapper](newDependencies),
		dix.ProviderErr1[fiber.Views, *Messages](NewTemplateEngine, dix.Into[fiber.Views](dix.Key("admin"), dix.Order(-80))),
		dix.Provider1[*Service, Dependencies](NewService, dix.Into[api.FiberRoute](dix.Key("admin"), dix.Order(-80))),
	),
)

func newBaseDependencies(
	cfg config.Config,
	metadata meta.Store,
	objects object.Store,
	version build.Version,
	logger *slog.Logger,
	authService *auth.Service,
) baseDependencies {
	return baseDependencies{
		Config:   cfg,
		Metadata: metadata,
		Objects:  objects,
		Version:  version,
		Logger:   logger,
		Auth:     authService,
	}
}

func newDependencies(
	base baseDependencies,
	syncer *scheduler.Runtime,
	runtimes *collectionlist.List[ecosystem.Runtime],
	messages *Messages,
	mapper *AdminMapper,
) Dependencies {
	return Dependencies{
		Config:    base.Config,
		Metadata:  base.Metadata,
		Objects:   base.Objects,
		Runtimes:  runtimes,
		Version:   base.Version,
		Logger:    base.Logger,
		Auth:      base.Auth,
		Messages:  messages,
		Mapper:    mapper,
		Syncer:    syncer,
		Prefetch:  syncer,
		Scheduler: syncer,
	}
}

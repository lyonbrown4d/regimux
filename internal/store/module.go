// Package store wires metadata and object storage modules.
package store

import (
	"context"
	"log/slog"

	"github.com/arcgolabs/dbx"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/samber/oops"
)

type MetadataStoreDependencies struct {
	Config  config.StoreMetaConfig
	Logger  *slog.Logger
	Metrics *observability.Metrics
}

var Module = dix.NewModule("store",
	dix.Providers(
		dix.Provider1[config.StoreMetaConfig, config.Config](func(cfg config.Config) config.StoreMetaConfig {
			return cfg.Store.Meta
		}),
		dix.Provider1[config.StoreObjectConfig, config.Config](func(cfg config.Config) config.StoreObjectConfig {
			return cfg.Store.Object
		}),
		dix.Provider3[MetadataStoreDependencies, config.StoreMetaConfig, *slog.Logger, *observability.Metrics](newMetadataStoreDependencies),
		dix.ProviderErr1[meta.Store, MetadataStoreDependencies](NewMetadataStore, dix.Eager()),
		dix.ProviderErr1[object.Store, config.StoreObjectConfig](NewObjectStore, dix.Eager()),
	),
	dix.Hooks(
		dix.OnStop[meta.Store](
			closeMetadataStore,
			dix.LifecycleName("regimux.meta_store_close"),
			dix.LifecyclePriority(-160),
		),
	),
)

func newMetadataStoreDependencies(cfg config.StoreMetaConfig, logger *slog.Logger, metrics *observability.Metrics) MetadataStoreDependencies {
	return MetadataStoreDependencies{
		Config:  cfg,
		Logger:  logger,
		Metrics: metrics,
	}
}

func NewMetadataStore(deps MetadataStoreDependencies) (meta.Store, error) {
	cfg := deps.Config
	switch cfg.Driver {
	case "sqlite", "mysql", "postgres":
	default:
		return nil, oops.In("store").With("driver", cfg.Driver).Errorf("unsupported metadata store driver")
	}
	store, err := meta.OpenDBWithOptions(context.Background(), meta.DBOptions{
		Driver: cfg.Driver,
		DSN:    cfg.DSN,
		Path:   cfg.Path,
		Logger: deps.Logger,
		Hooks:  metadataDBHooks(deps.Metrics, cfg.Driver),
	})
	if err != nil {
		return nil, oops.Wrapf(err, "open metadata store")
	}
	return store, nil
}

func metadataDBHooks(metrics *observability.Metrics, driver string) []dbx.Hook {
	hook := observability.NewDBMetricsHook(metrics, driver)
	if hook == nil {
		return nil
	}
	return []dbx.Hook{hook}
}

func NewObjectStore(cfg config.StoreObjectConfig) (object.Store, error) {
	store, err := object.New(cfg.Driver, cfg.Path)
	if err != nil {
		return nil, oops.Wrapf(err, "create object store")
	}
	return store, nil
}

func closeMetadataStore(_ context.Context, store meta.Store) error {
	if store == nil {
		return nil
	}
	if err := store.Close(); err != nil {
		return oops.Wrapf(err, "close metadata store")
	}
	return nil
}

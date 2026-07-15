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

type ObjectStoreDependencies struct {
	Config config.StoreObjectConfig
	Logger *slog.Logger
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
		dix.Provider2[ObjectStoreDependencies, config.StoreObjectConfig, *slog.Logger](newObjectStoreDependencies),
		dix.Provider0[*meta.MetadataMapper](meta.NewMetadataMapper),
		dix.ProviderErr1[*dbx.DB, MetadataStoreDependencies](NewMetadataDB, dix.Eager()),
		dix.Provider2[meta.Store, *dbx.DB, *meta.MetadataMapper](NewMetadataStore, dix.Eager()),
		dix.ProviderErr1[object.Store, ObjectStoreDependencies](NewObjectStore, dix.Eager()),
	),
	dix.Hooks(
		dix.OnStop2[*dbx.DB, *slog.Logger](
			closeMetadataDB,
			dix.LifecycleName("regimux.metadata_db_close"),
			dix.LifecyclePriority(-160),
		),
		dix.OnStop2[object.Store, *slog.Logger](
			closeObjectStore,
			dix.LifecycleName("regimux.object_store_close"),
			dix.LifecyclePriority(-150),
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

func newObjectStoreDependencies(cfg config.StoreObjectConfig, logger *slog.Logger) ObjectStoreDependencies {
	return ObjectStoreDependencies{
		Config: cfg,
		Logger: logger,
	}
}

func NewMetadataDB(deps MetadataStoreDependencies) (*dbx.DB, error) {
	cfg := deps.Config
	logger := storeLogger(deps.Logger, "store.meta")
	logger.Info("opening metadata store", "driver", cfg.Driver, "path", cfg.Path, "dsn_configured", cfg.DSN != "")
	switch cfg.Driver {
	case "sqlite", "mysql", "postgres":
	default:
		return nil, oops.In("store").With("driver", cfg.Driver).Errorf("unsupported metadata store driver")
	}
	db, err := meta.OpenMetadataDB(context.Background(), meta.DBOptions{
		Driver: cfg.Driver,
		DSN:    cfg.DSN,
		Path:   cfg.Path,
		Logger: deps.Logger,
		Hooks:  metadataDBHooks(deps.Metrics, cfg.Driver),
	})
	if err != nil {
		return nil, oops.Wrapf(err, "open metadata store")
	}
	logger.Info("metadata store opened", "driver", cfg.Driver)
	return db, nil
}

func NewMetadataStore(db *dbx.DB, mapper *meta.MetadataMapper) meta.Store {
	return meta.NewSQLStore(db, mapper)
}

func metadataDBHooks(metrics *observability.Metrics, driver string) []dbx.Hook {
	hook := observability.NewDBMetricsHook(metrics, driver)
	if hook == nil {
		return nil
	}
	return []dbx.Hook{hook}
}

func NewObjectStore(deps ObjectStoreDependencies) (object.Store, error) {
	cfg := deps.Config
	logger := storeLogger(deps.Logger, "store.object")
	store, err := object.NewWithOptions(context.Background(), object.Options{
		Driver: cfg.Driver,
		Path:   cfg.Path,
		S3: object.S3Options{
			Bucket:            cfg.S3.Bucket,
			Prefix:            cfg.S3.Prefix,
			Region:            cfg.S3.Region,
			Endpoint:          cfg.S3.Endpoint,
			AccessKeyID:       cfg.S3.AccessKeyID,
			SecretAccessKey:   cfg.S3.SecretAccessKey,
			SessionToken:      cfg.S3.SessionToken,
			Profile:           cfg.S3.Profile,
			ForcePathStyle:    cfg.S3.ForcePathStyle,
			PartSize:          cfg.S3.PartSize,
			UploadConcurrency: cfg.S3.UploadConcurrency,
		},
	})
	if err != nil {
		return nil, oops.Wrapf(err, "create object store")
	}
	logger.Info("object store opened", "driver", cfg.Driver)
	return store, nil
}

func closeObjectStore(_ context.Context, store object.Store, logger *slog.Logger) error {
	closer, ok := store.(interface{ Close() error })
	if !ok {
		return nil
	}
	logger = storeLogger(logger, "store.object")
	logger.Info("closing object store")
	if err := closer.Close(); err != nil {
		return oops.Wrapf(err, "close object store")
	}
	logger.Info("object store closed")
	return nil
}

func closeMetadataDB(_ context.Context, db *dbx.DB, logger *slog.Logger) error {
	if db == nil {
		return nil
	}
	logger = storeLogger(logger, "store.meta")
	logger.Info("closing metadata db")
	if err := db.Close(); err != nil {
		return oops.Wrapf(err, "close metadata db")
	}
	logger.Info("metadata db closed")
	return nil
}

func storeLogger(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With("component", component)
}

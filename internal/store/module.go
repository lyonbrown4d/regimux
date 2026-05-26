// Package store wires metadata and object storage modules.
package store

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func Module(configModule, observabilityModule dix.Module) dix.Module {
	return dix.NewModule("store",
		dix.Imports(configModule, observabilityModule),
		dix.Providers(
			dix.Provider2[meta.Store, config.Config, *slog.Logger](newMetadataStore, dix.Eager()),
			dix.Provider1[object.Store, config.Config](newObjectStore, dix.Eager()),
		),
	)
}

func newMetadataStore(cfg config.Config, logger *slog.Logger) meta.Store {
	store, err := meta.OpenBbolt(cfg.Store.Meta.Path, logger)
	if err != nil {
		logger.Error("open metadata store failed", "path", cfg.Store.Meta.Path, "error", err)
		return nil
	}
	return store
}

func newObjectStore(cfg config.Config) object.Store {
	store, err := object.NewLocal(cfg.Store.Object.Path)
	if err != nil {
		return nil
	}
	return store
}

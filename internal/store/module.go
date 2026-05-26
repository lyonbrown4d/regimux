// Package store wires metadata and object storage modules.
package store

import (
	"context"
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/samber/oops"
)

func Module() dix.Module {
	return dix.NewModule("store",
		dix.Providers(
			dix.ProviderErr2[meta.Store, config.Config, *slog.Logger](newMetadataStore, dix.Eager()),
			dix.ProviderErr1[object.Store, config.Config](newObjectStore, dix.Eager()),
		),
		dix.Hooks(
			dix.OnStop[meta.Store](
				closeMetadataStore,
				dix.LifecycleName("regimux.meta_store_close"),
				dix.LifecyclePriority(-160),
			),
		),
	)
}

func newMetadataStore(cfg config.Config, logger *slog.Logger) (meta.Store, error) {
	store, err := meta.OpenBbolt(cfg.Store.Meta.Path, logger)
	if err != nil {
		return nil, oops.Wrapf(err, "open metadata store")
	}
	return store, nil
}

func newObjectStore(cfg config.Config) (object.Store, error) {
	store, err := object.New(cfg.Store.Object.Driver, cfg.Store.Object.Path)
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

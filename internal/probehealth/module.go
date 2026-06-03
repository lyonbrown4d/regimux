package probehealth

import (
	"context"
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/oops"
)

var Module = dix.NewModule("probehealth",
	dix.Providers(
		dix.Provider2[Store, config.Config, *slog.Logger](NewStore),
	),
	dix.Hooks(
		dix.OnStop[Store](closeStore, dix.LifecycleName("regimux.probehealth_close"), dix.LifecyclePriority(-145)),
	),
)

func closeStore(_ context.Context, store Store) error {
	if store == nil {
		return nil
	}
	if err := store.Close(); err != nil {
		return oops.In("probehealth").Wrapf(err, "close probe health hot store")
	}
	return nil
}

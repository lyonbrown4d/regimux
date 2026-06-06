package auth

import (
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
)

var Module = dix.NewModule("auth",
	dix.Providers(
		dix.ProviderErr3[*Service, config.Config, *slog.Logger, *collectionlist.List[ResourceResolver]](NewService),
	),
)

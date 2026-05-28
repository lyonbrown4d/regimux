package auth

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
)

var Module = dix.NewModule("auth",
	dix.Providers(
		dix.ProviderErr2[*Service, config.Config, *slog.Logger](NewService),
	),
)

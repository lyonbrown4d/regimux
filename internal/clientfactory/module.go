package clientfactory

import (
	"log/slog"

	"github.com/arcgolabs/dix"
)

var Module = dix.NewModule("clientfactory",
	dix.Providers(
		dix.Provider1[*Factory, *slog.Logger](New),
	),
)

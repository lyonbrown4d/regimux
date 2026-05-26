package events

import (
	"log/slog"

	"github.com/arcgolabs/dix"
)

func Module(observabilityModule dix.Module) dix.Module {
	return dix.NewModule("events",
		dix.Imports(observabilityModule),
		dix.Providers(
			dix.Provider1[Bus, *slog.Logger](NewBus, dix.Eager()),
		),
	)
}

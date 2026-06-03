package artifactcache

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

var Module = dix.NewModule("artifact-cache",
	dix.Providers(
		dix.Provider3[Dependencies, meta.Store, object.Store, *slog.Logger](newDependencies),
		dix.Provider1[*Store, Dependencies](New),
	),
)

func newDependencies(metadata meta.Store, objects object.Store, logger *slog.Logger) Dependencies {
	return Dependencies{
		Metadata: metadata,
		Objects:  objects,
		Logger:   logger,
	}
}

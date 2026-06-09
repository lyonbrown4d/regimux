package artifactcache

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

var Module = dix.NewModule("artifact-cache",
	dix.Providers(
		dix.Provider3[*Store, meta.Store, object.Store, *slog.Logger](
			func(metadata meta.Store, objects object.Store, logger *slog.Logger) *Store {
				return New(Dependencies{
					Metadata: metadata,
					Objects:  objects,
					Logger:   logger,
				})
			},
		),
	),
)

package suggestion

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

var Module = dix.NewModule("suggestion",
	dix.Providers(
		dix.Provider1[Options, config.Config](OptionsFromConfig),
		dix.Provider4[*Service, upstream.RegistryClient, backend.Backend, Options, *slog.Logger](NewServiceFromParts),
		dix.Provider1[ManifestService, *Service](func(service *Service) ManifestService {
			return service
		}),
	),
)

package ecosystem

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/probehealth"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("ecosystem",
	dix.Providers(
		dix.Provider5[*EndpointProber, meta.Store, probehealth.Store, *worker.Pools, *clientfactory.Factory, *slog.Logger](newEndpointProber),
	),
)

func newEndpointProber(
	metadata meta.Store,
	hotHealth probehealth.Store,
	pools *worker.Pools,
	factory *clientfactory.Factory,
	logger *slog.Logger,
) *EndpointProber {
	return NewEndpointProberWithFactory(metadata, pools, logger, factory, hotHealth)
}

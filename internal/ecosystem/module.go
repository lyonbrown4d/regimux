package ecosystem

import (
	"log/slog"

	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/probehealth"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
)

var Module = dix.NewModule("ecosystem",
	dix.Providers(
		dix.Provider4[*EndpointProber, meta.Store, probehealth.Store, *worker.Pools, *slog.Logger](newEndpointProber),
	),
)

func newEndpointProber(metadata meta.Store, hotHealth probehealth.Store, pools *worker.Pools, logger *slog.Logger) *EndpointProber {
	return NewEndpointProber(metadata, pools, logger, hotHealth)
}

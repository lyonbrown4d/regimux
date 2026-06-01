package observability

import (
	"log/slog"
	"net/http"

	"github.com/arcgolabs/observabilityx"
	promobs "github.com/arcgolabs/observabilityx/prometheus"
)

const (
	metricsNamespace = "regimux"
	metricsSubsystem = "service"
)

type Metrics struct {
	handler http.Handler

	api       apiMetrics
	cache     cacheMetrics
	config    configMetrics
	db        dbMetrics
	docker    dockerMetrics
	scheduler schedulerMetrics
	upstream  upstreamMetrics
}

func NewMetrics(logger *slog.Logger) *Metrics {
	adapter := promobs.New(
		promobs.WithLogger(logger),
		promobs.WithNamespace(metricsNamespace),
	)
	return NewMetricsFromObservability(adapter, adapter.Handler())
}

func NewMetricsFromObservability(obs observabilityx.Observability, handler http.Handler) *Metrics {
	obs = observabilityx.Normalize(obs, slog.Default())
	return &Metrics{
		handler:   handler,
		api:       newAPIMetrics(obs),
		cache:     newCacheMetrics(obs),
		config:    newConfigMetrics(obs),
		db:        newDBMetrics(obs),
		docker:    newDockerMetrics(obs),
		scheduler: newSchedulerMetrics(obs),
		upstream:  newUpstreamMetrics(obs),
	}
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.handler == nil {
		return http.NotFoundHandler()
	}
	return m.handler
}

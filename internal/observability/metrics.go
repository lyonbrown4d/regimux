package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/arcgolabs/observabilityx"
	promobs "github.com/arcgolabs/observabilityx/prometheus"
)

const (
	metricsNamespace = "regimux"
	metricsSubsystem = "service"
)

type Metrics struct {
	handler http.Handler

	upstreamRequests        observabilityx.Counter
	upstreamRequestDuration observabilityx.Histogram
	upstreamFailovers       observabilityx.Counter
	cacheAccesses           observabilityx.Counter
	cacheStores             observabilityx.Counter
	apiRequests             observabilityx.Counter
	apiRequestDuration      observabilityx.Histogram
	schedulerJobs           observabilityx.Counter
	schedulerJobDuration    observabilityx.Histogram
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
		handler: handler,

		upstreamRequests: obs.Counter(counterSpec(
			"upstream_requests_total",
			"Total upstream requests.",
			"alias", "operation", "method", "registry", "status", "result", "attempts",
		)),
		upstreamRequestDuration: obs.Histogram(durationHistogramSpec(
			"upstream_request_duration_seconds",
			"Upstream request duration in seconds.",
			"alias", "operation", "method", "registry", "result",
		)),
		upstreamFailovers: obs.Counter(counterSpec(
			"upstream_failovers_total",
			"Total upstream failover attempts.",
			"alias", "operation", "registry", "has_next", "result",
		)),
		cacheAccesses: obs.Counter(counterSpec(
			"cache_accesses_total",
			"Total cache accesses.",
			"kind", "alias", "repository", "status",
		)),
		cacheStores: obs.Counter(counterSpec(
			"cache_store_bytes_total",
			"Total bytes stored in cache.",
			"kind", "alias", "repository",
		)),
		apiRequests: obs.Counter(counterSpec(
			"api_requests_total",
			"Total API requests.",
			"route", "method", "status", "result",
		)),
		apiRequestDuration: obs.Histogram(durationHistogramSpec(
			"api_request_duration_seconds",
			"API request duration in seconds.",
			"route", "method", "result",
		)),
		schedulerJobs: obs.Counter(counterSpec(
			"scheduler_jobs_total",
			"Total scheduler jobs.",
			"job", "alias", "result",
		)),
		schedulerJobDuration: obs.Histogram(durationHistogramSpec(
			"scheduler_job_duration_seconds",
			"Scheduler job duration in seconds.",
			"job", "alias", "result",
		)),
	}
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.handler == nil {
		return http.NotFoundHandler()
	}
	return m.handler
}

func (m *Metrics) ObserveUpstreamRequest(ctx context.Context, alias, operation, method, registry string, status, attempts int, duration time.Duration, err error) {
	if m == nil {
		return
	}
	if attempts <= 0 {
		attempts = 1
	}

	result := resultLabel(err, status)
	m.upstreamRequests.Add(ctx, 1,
		observabilityx.String("alias", alias),
		observabilityx.String("operation", operation),
		observabilityx.String("method", method),
		observabilityx.String("registry", registry),
		observabilityx.String("status", strconv.Itoa(status)),
		observabilityx.String("result", result),
		observabilityx.String("attempts", strconv.Itoa(attempts)),
	)
	m.upstreamRequestDuration.Record(ctx, duration.Seconds(),
		observabilityx.String("alias", alias),
		observabilityx.String("operation", operation),
		observabilityx.String("method", method),
		observabilityx.String("registry", registry),
		observabilityx.String("result", result),
	)
}

func (m *Metrics) ObserveUpstreamFailover(ctx context.Context, alias, operation, registry string, hasNext bool, err error) {
	if m == nil {
		return
	}
	m.upstreamFailovers.Add(ctx, 1,
		observabilityx.String("alias", alias),
		observabilityx.String("operation", operation),
		observabilityx.String("registry", registry),
		observabilityx.String("has_next", boolLabel(hasNext)),
		observabilityx.String("result", resultLabel(err, 0)),
	)
}

func (m *Metrics) ObserveCacheAccess(ctx context.Context, kind, alias, repository, status string) {
	if m == nil {
		return
	}
	m.cacheAccesses.Add(ctx, 1,
		observabilityx.String("kind", kind),
		observabilityx.String("alias", alias),
		observabilityx.String("repository", repository),
		observabilityx.String("status", status),
	)
}

func (m *Metrics) ObserveCacheStore(ctx context.Context, kind, alias, repository string, size int64) {
	if m == nil {
		return
	}
	if size < 0 {
		size = 0
	}
	m.cacheStores.Add(ctx, size,
		observabilityx.String("kind", kind),
		observabilityx.String("alias", alias),
		observabilityx.String("repository", repository),
	)
}

func (m *Metrics) ObserveAPIRequest(ctx context.Context, route, method string, status int, duration time.Duration, err error) {
	if m == nil {
		return
	}

	result := resultLabel(err, status)
	m.apiRequests.Add(ctx, 1,
		observabilityx.String("route", route),
		observabilityx.String("method", method),
		observabilityx.String("status", strconv.Itoa(status)),
		observabilityx.String("result", result),
	)
	m.apiRequestDuration.Record(ctx, duration.Seconds(),
		observabilityx.String("route", route),
		observabilityx.String("method", method),
		observabilityx.String("result", result),
	)
}

func (m *Metrics) ObserveSchedulerJob(ctx context.Context, job, alias string, duration time.Duration, err error) {
	if m == nil {
		return
	}

	result := resultLabel(err, 0)
	m.schedulerJobs.Add(ctx, 1,
		observabilityx.String("job", job),
		observabilityx.String("alias", alias),
		observabilityx.String("result", result),
	)
	m.schedulerJobDuration.Record(ctx, duration.Seconds(),
		observabilityx.String("job", job),
		observabilityx.String("alias", alias),
		observabilityx.String("result", result),
	)
}

func counterSpec(name, description string, labels ...string) observabilityx.CounterSpec {
	return observabilityx.NewCounterSpec(
		metricsSubsystem+"_"+name,
		observabilityx.WithDescription(description),
		observabilityx.WithLabelKeys(labels...),
	)
}

func durationHistogramSpec(name, description string, labels ...string) observabilityx.HistogramSpec {
	return observabilityx.NewHistogramSpec(
		metricsSubsystem+"_"+name,
		observabilityx.WithDescription(description),
		observabilityx.WithUnit("s"),
		observabilityx.WithLabelKeys(labels...),
	).WithBuckets(exponentialBuckets(0.001, 2, 15)...)
}

func exponentialBuckets(start, factor float64, count int) []float64 {
	if start <= 0 || factor <= 1 || count <= 0 {
		return nil
	}
	buckets := make([]float64, count)
	current := start
	for i := range count {
		buckets[i] = current
		current *= factor
	}
	return buckets
}

func resultLabel(err error, status int) string {
	if err != nil || status >= 400 {
		return "error"
	}
	return "success"
}

func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

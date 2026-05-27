package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/samber/oops"
)

const (
	metricsNamespace = "regimux"
	metricsSubsystem = "service"
)

type Metrics struct {
	registry *prometheus.Registry

	upstreamRequests        *prometheus.CounterVec
	upstreamRequestDuration *prometheus.HistogramVec
	upstreamFailovers       *prometheus.CounterVec
	cacheAccesses           *prometheus.CounterVec
	cacheStores             *prometheus.CounterVec
	apiRequests             *prometheus.CounterVec
	apiRequestDuration      *prometheus.HistogramVec
	schedulerJobs           *prometheus.CounterVec
	schedulerJobDuration    *prometheus.HistogramVec
}

func NewMetrics(_ *slog.Logger) *Metrics {
	registry := prometheus.NewRegistry()
	registerCollectors(registry,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	metrics := &Metrics{
		registry:                registry,
		upstreamRequests:        upstreamRequests(registry),
		upstreamRequestDuration: upstreamRequestDuration(registry),
		upstreamFailovers:       upstreamFailovers(registry),
		cacheAccesses:           cacheAccesses(registry),
		cacheStores:             cacheStores(registry),
		apiRequests:             apiRequests(registry),
		apiRequestDuration:      apiRequestDuration(registry),
		schedulerJobs:           schedulerJobs(registry),
		schedulerJobDuration:    schedulerJobDuration(registry),
	}
	return metrics
}

func (m *Metrics) ObserveUpstreamRequest(alias, operation, method, registry string, status, attempts int, duration time.Duration, err error) {
	if m == nil {
		return
	}
	if attempts <= 0 {
		attempts = 1
	}
	result := resultLabel(err, status)
	statusText := strconv.Itoa(status)
	m.upstreamRequests.WithLabelValues(alias, operation, method, registry, statusText, result, strconv.Itoa(attempts)).Inc()
	m.upstreamRequestDuration.WithLabelValues(alias, operation, method, registry, result).Observe(duration.Seconds())
}

func (m *Metrics) ObserveUpstreamFailover(alias, operation, registry string, hasNext bool, err error) {
	if m == nil {
		return
	}
	m.upstreamFailovers.WithLabelValues(alias, operation, registry, boolLabel(hasNext), resultLabel(err, 0)).Inc()
}

func (m *Metrics) ObserveCacheAccess(kind, alias, repository, status string) {
	if m == nil {
		return
	}
	m.cacheAccesses.WithLabelValues(kind, alias, repository, status).Inc()
}

func (m *Metrics) ObserveCacheStore(kind, alias, repository string, size int64) {
	if m == nil {
		return
	}
	if size < 0 {
		size = 0
	}
	m.cacheStores.WithLabelValues(kind, alias, repository).Add(float64(size))
}

func (m *Metrics) ObserveAPIRequest(route, method string, status int, duration time.Duration, err error) {
	if m == nil {
		return
	}
	result := resultLabel(err, status)
	statusText := strconv.Itoa(status)
	m.apiRequests.WithLabelValues(route, method, statusText, result).Inc()
	m.apiRequestDuration.WithLabelValues(route, method, result).Observe(duration.Seconds())
}

func (m *Metrics) ObserveSchedulerJob(job, alias string, duration time.Duration, err error) {
	if m == nil {
		return
	}
	result := resultLabel(err, 0)
	m.schedulerJobs.WithLabelValues(job, alias, result).Inc()
	m.schedulerJobDuration.WithLabelValues(job, alias, result).Observe(duration.Seconds())
}

func (m *Metrics) Text() ([]byte, error) {
	families, err := m.Gather()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	encoder := expfmt.NewEncoder(&out, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, family := range families {
		if err := encoder.Encode(family); err != nil {
			return nil, oops.Wrapf(err, "encode metrics text")
		}
	}
	return out.Bytes(), nil
}

func (m *Metrics) Gather() ([]*dto.MetricFamily, error) {
	if m == nil || m.registry == nil {
		return nil, nil
	}
	families, err := m.registry.Gather()
	if err != nil {
		return nil, oops.Wrapf(err, "gather metrics")
	}
	return families, nil
}

func NewUpstreamMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(_ context.Context, event events.UpstreamRequest) error {
		metrics.ObserveUpstreamRequest(
			event.Alias,
			event.Operation,
			event.Method,
			event.Registry,
			event.Status,
			event.Attempts,
			event.Duration,
			errorFromMessage(event.Error),
		)
		return nil
	})
}

func NewFailoverMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(_ context.Context, event events.UpstreamFailover) error {
		metrics.ObserveUpstreamFailover(event.Alias, event.Operation, event.Registry, event.HasNext, errorFromMessage(event.Error))
		return nil
	})
}

func NewCacheAccessMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(_ context.Context, event events.CacheAccess) error {
		metrics.ObserveCacheAccess(event.Kind, event.Alias, event.Repository, event.Status)
		return nil
	})
}

func NewCacheStoreMetricsSubscriber(metrics *Metrics) events.Subscriber {
	if metrics == nil {
		return nil
	}
	return events.NewSubscriber(func(_ context.Context, event events.CacheStore) error {
		metrics.ObserveCacheStore(event.Kind, event.Alias, event.Repository, event.Size)
		return nil
	})
}

func upstreamRequests(registry *prometheus.Registry) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "upstream_requests_total",
		Help:      "Total upstream requests.",
	}, []string{"alias", "operation", "method", "registry", "status", "result", "attempts"})
	registerCollectors(registry, counter)
	return counter
}

func upstreamRequestDuration(registry *prometheus.Registry) *prometheus.HistogramVec {
	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "upstream_request_duration_seconds",
		Help:      "Upstream request duration in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15),
	}, []string{"alias", "operation", "method", "registry", "result"})
	registerCollectors(registry, histogram)
	return histogram
}

func upstreamFailovers(registry *prometheus.Registry) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "upstream_failovers_total",
		Help:      "Total upstream failover attempts.",
	}, []string{"alias", "operation", "registry", "has_next", "result"})
	registerCollectors(registry, counter)
	return counter
}

func cacheAccesses(registry *prometheus.Registry) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "cache_accesses_total",
		Help:      "Total cache accesses.",
	}, []string{"kind", "alias", "repository", "status"})
	registerCollectors(registry, counter)
	return counter
}

func cacheStores(registry *prometheus.Registry) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "cache_store_bytes_total",
		Help:      "Total bytes stored in cache.",
	}, []string{"kind", "alias", "repository"})
	registerCollectors(registry, counter)
	return counter
}

func apiRequests(registry *prometheus.Registry) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "api_requests_total",
		Help:      "Total API requests.",
	}, []string{"route", "method", "status", "result"})
	registerCollectors(registry, counter)
	return counter
}

func apiRequestDuration(registry *prometheus.Registry) *prometheus.HistogramVec {
	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "api_request_duration_seconds",
		Help:      "API request duration in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15),
	}, []string{"route", "method", "result"})
	registerCollectors(registry, histogram)
	return histogram
}

func schedulerJobs(registry *prometheus.Registry) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "scheduler_jobs_total",
		Help:      "Total scheduler jobs.",
	}, []string{"job", "alias", "result"})
	registerCollectors(registry, counter)
	return counter
}

func schedulerJobDuration(registry *prometheus.Registry) *prometheus.HistogramVec {
	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: metricsSubsystem,
		Name:      "scheduler_job_duration_seconds",
		Help:      "Scheduler job duration in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15),
	}, []string{"job", "alias", "result"})
	registerCollectors(registry, histogram)
	return histogram
}

func registerCollectors(registry *prometheus.Registry, metricCollectors ...prometheus.Collector) {
	if registry == nil {
		return
	}
	for _, collector := range metricCollectors {
		if collector == nil {
			continue
		}
		if err := registry.Register(collector); err != nil {
			var alreadyRegistered prometheus.AlreadyRegisteredError
			if !errors.As(err, &alreadyRegistered) {
				continue
			}
		}
	}
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

func errorFromMessage(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	return metricEventError(message)
}

type metricEventError string

func (e metricEventError) Error() string {
	return string(e)
}

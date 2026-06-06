package observability

import (
	"context"
	"strconv"
	"time"

	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type upstreamMetrics struct {
	requests            observabilityx.Counter
	requestDuration     observabilityx.Histogram
	responseBytes       observabilityx.Histogram
	failovers           observabilityx.Counter
	endpointConfigured  observabilityx.Gauge
	endpointCooldown    observabilityx.Gauge
	endpointDegraded    observabilityx.Gauge
	endpointFailures    observabilityx.Gauge
	endpointInflight    observabilityx.Gauge
	endpointLatency     observabilityx.Gauge
	endpointScore       observabilityx.Gauge
	endpointSuccessRate observabilityx.Gauge
}

func newUpstreamMetrics(obs observabilityx.Observability) upstreamMetrics {
	return upstreamMetrics{
		requests:            newUpstreamRequests(obs),
		requestDuration:     newUpstreamRequestDuration(obs),
		responseBytes:       newUpstreamResponseBytes(obs),
		failovers:           newUpstreamFailovers(obs),
		endpointConfigured:  newUpstreamEndpointGauge(obs, "upstream_endpoint_configured", "Configured upstream endpoint."),
		endpointCooldown:    newUpstreamEndpointGauge(obs, "upstream_endpoint_cooldown", "Whether the upstream endpoint is in cooldown."),
		endpointDegraded:    newUpstreamEndpointGauge(obs, "upstream_endpoint_degraded", "Whether the upstream endpoint is degraded."),
		endpointFailures:    newUpstreamEndpointGauge(obs, "upstream_endpoint_consecutive_failures", "Consecutive upstream endpoint failures."),
		endpointInflight:    newUpstreamEndpointGauge(obs, "upstream_endpoint_inflight", "In-flight blob requests for the upstream endpoint."),
		endpointLatency:     newUpstreamEndpointGauge(obs, "upstream_endpoint_latency_seconds", "Upstream endpoint latency EWMA in seconds."),
		endpointScore:       newUpstreamEndpointGauge(obs, "upstream_endpoint_score_seconds", "Upstream endpoint scheduler score in seconds."),
		endpointSuccessRate: newUpstreamEndpointGauge(obs, "upstream_endpoint_success_rate", "Upstream endpoint success rate."),
	}
}

func (m *Metrics) ObserveUpstreamRequest(ctx context.Context, req UpstreamRequestMetric) {
	if m == nil {
		return
	}
	if req.Attempts <= 0 {
		req.Attempts = 1
	}

	result := resultLabel(req.Err, req.Status)
	labels := upstreamRequestLabels(req, result)
	m.upstream.requests.Add(ctx, 1, append(labels,
		observabilityx.String("status", strconv.Itoa(req.Status)),
		observabilityx.String("attempts", strconv.Itoa(req.Attempts)),
	)...)
	m.upstream.requestDuration.Record(ctx, req.Duration.Seconds(), labels...)
	if req.Size >= 0 {
		m.upstream.responseBytes.Record(ctx, float64(req.Size), labels...)
	}
}

func (m *Metrics) ObserveUpstreamFailover(ctx context.Context, alias, operation, registry string, hasNext bool, err error) {
	if m == nil {
		return
	}
	m.upstream.failovers.Add(ctx, 1,
		observabilityx.String("alias", alias),
		observabilityx.String("operation", operation),
		observabilityx.String("registry", registry),
		observabilityx.String("has_next", boolLabel(hasNext)),
		observabilityx.String("result", resultLabel(err, 0)),
	)
}

func (m *Metrics) ObserveUpstreamSnapshot(ctx context.Context, snapshot ecosystem.ClientSnapshot) {
	if m == nil {
		return
	}
	snapshot.Upstreams.Range(func(_ int, upstreamSnapshot ecosystem.UpstreamSnapshot) bool {
		upstreamSnapshot.Endpoints.Range(func(_ int, endpoint ecosystem.EndpointSnapshot) bool {
			m.observeEndpointSnapshot(ctx, upstreamSnapshot, endpoint)
			return true
		})
		return true
	})
}

type UpstreamRequestMetric struct {
	Alias     string
	Operation string
	Registry  string
	Method    string
	Status    int
	Attempts  int
	Duration  time.Duration
	Size      int64
	Err       error
}

package observability

import (
	"context"

	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func newUpstreamRequests(obs observabilityx.Observability) observabilityx.Counter {
	return obs.Counter(counterSpec(
		"upstream_requests_total",
		"Total upstream requests.",
		"alias", "operation", "method", "registry", "result", "status", "attempts",
	))
}

func newUpstreamRequestDuration(obs observabilityx.Observability) observabilityx.Histogram {
	return obs.Histogram(durationHistogramSpec(
		"upstream_request_duration_seconds",
		"Upstream request duration in seconds.",
		"alias", "operation", "method", "registry", "result",
	))
}

func newUpstreamResponseBytes(obs observabilityx.Observability) observabilityx.Histogram {
	return obs.Histogram(bytesHistogramSpec(
		"upstream_response_bytes",
		"Upstream response body size in bytes.",
		"alias", "operation", "method", "registry", "result",
	))
}

func newUpstreamFailovers(obs observabilityx.Observability) observabilityx.Counter {
	return obs.Counter(counterSpec(
		"upstream_failovers_total",
		"Total upstream failover attempts.",
		"alias", "operation", "registry", "has_next", "result",
	))
}

func newUpstreamEndpointGauge(obs observabilityx.Observability, name, description string) observabilityx.Gauge {
	return obs.Gauge(gaugeSpec(name, description, "alias", "registry", "role"))
}

func upstreamRequestLabels(req UpstreamRequestMetric, result string) []observabilityx.Attribute {
	return []observabilityx.Attribute{
		observabilityx.String("alias", req.Alias),
		observabilityx.String("operation", req.Operation),
		observabilityx.String("method", req.Method),
		observabilityx.String("registry", req.Registry),
		observabilityx.String("result", result),
	}
}

func (m *Metrics) observeEndpointSnapshot(ctx context.Context, snapshot upstream.UpstreamSnapshot, endpoint upstream.EndpointSnapshot) {
	labels := endpointLabels(snapshot.Alias, endpoint)
	health := endpoint.Health
	m.upstream.endpointConfigured.Set(ctx, 1, labels...)
	m.upstream.endpointCooldown.Set(ctx, boolFloat(health.InCooldown), labels...)
	m.upstream.endpointDegraded.Set(ctx, boolFloat(health.InDegraded), labels...)
	m.upstream.endpointFailures.Set(ctx, float64(health.ConsecutiveFailures), labels...)
	m.upstream.endpointInflight.Set(ctx, float64(health.Inflight), labels...)
	m.upstream.endpointScore.Set(ctx, health.Score.Seconds(), labels...)
	if health.HasLatency {
		m.upstream.endpointLatency.Set(ctx, health.LatencyEWMA.Seconds(), labels...)
	}
	if health.HasSuccessRate {
		m.upstream.endpointSuccessRate.Set(ctx, health.SuccessRate, labels...)
	}
}

func endpointLabels(alias string, endpoint upstream.EndpointSnapshot) []observabilityx.Attribute {
	return []observabilityx.Attribute{
		observabilityx.String("alias", alias),
		observabilityx.String("registry", endpoint.Registry),
		observabilityx.String("role", endpoint.Role),
	}
}

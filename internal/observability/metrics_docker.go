package observability

import (
	"context"
	"time"

	"github.com/arcgolabs/observabilityx"
)

type dockerMetrics struct {
	daemonConnected observabilityx.Gauge
	imageEvents     observabilityx.Counter
	prewarmRequests observabilityx.Counter
	prewarmDuration observabilityx.Histogram
}

func newDockerMetrics(obs observabilityx.Observability) dockerMetrics {
	return dockerMetrics{
		daemonConnected: obs.Gauge(gaugeSpec(
			"docker_daemon_connected",
			"Whether the optional Docker daemon integration is connected.",
			"host",
		)),
		imageEvents: obs.Counter(counterSpec(
			"docker_image_events_total",
			"Docker image events observed through the optional Docker daemon integration.",
			"action",
		)),
		prewarmRequests: obs.Counter(counterSpec(
			"docker_prewarm_requests_total",
			"Docker prewarm pull attempts started through the optional Docker daemon integration.",
			"alias", "status",
		)),
		prewarmDuration: obs.Histogram(durationHistogramSpec(
			"docker_prewarm_duration_seconds",
			"Docker prewarm pull duration through the optional Docker daemon integration.",
			"alias", "status",
		)),
	}
}

func (m *Metrics) ObserveDockerDaemon(ctx context.Context, host string, connected bool) {
	if m == nil {
		return
	}
	m.docker.daemonConnected.Set(ctx, boolFloat(connected), observabilityx.String("host", labelOrUnknown(host)))
}

func (m *Metrics) ObserveDockerImageEvent(ctx context.Context, action string) {
	if m == nil {
		return
	}
	m.docker.imageEvents.Add(ctx, 1, observabilityx.String("action", labelOrUnknown(action)))
}

func (m *Metrics) ObserveDockerPrewarm(ctx context.Context, alias string, duration time.Duration, err error) {
	if m == nil {
		return
	}
	status := "success"
	if err != nil {
		status = "error"
	}
	attrs := []observabilityx.Attribute{
		observabilityx.String("alias", labelOrUnknown(alias)),
		observabilityx.String("status", status),
	}
	m.docker.prewarmRequests.Add(ctx, 1, attrs...)
	m.docker.prewarmDuration.Record(ctx, duration.Seconds(), attrs...)
}

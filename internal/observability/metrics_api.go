package observability

import (
	"context"
	"strconv"
	"time"

	"github.com/arcgolabs/observabilityx"
)

type apiMetrics struct {
	requests        observabilityx.Counter
	requestDuration observabilityx.Histogram
	responseBytes   observabilityx.Histogram
}

func newAPIMetrics(obs observabilityx.Observability) apiMetrics {
	return apiMetrics{
		requests: obs.Counter(counterSpec(
			"api_requests_total",
			"Total API requests.",
			"route", "method", "status", "result",
		)),
		requestDuration: obs.Histogram(durationHistogramSpec(
			"api_request_duration_seconds",
			"API request duration in seconds.",
			"route", "method", "result",
		)),
		responseBytes: obs.Histogram(bytesHistogramSpec(
			"api_response_bytes",
			"API response body size in bytes.",
			"route", "method", "result",
		)),
	}
}

type APIRequestMetric struct {
	Route    string
	Method   string
	Status   int
	Duration time.Duration
	Size     int64
	Err      error
}

func (m *Metrics) ObserveAPIRequest(ctx context.Context, metric APIRequestMetric) {
	route := metric.Route
	method := metric.Method
	status := metric.Status
	duration := metric.Duration
	size := metric.Size
	err := metric.Err
	if m == nil {
		return
	}

	result := resultLabel(err, status)
	labels := []observabilityx.Attribute{
		observabilityx.String("route", route),
		observabilityx.String("method", method),
		observabilityx.String("result", result),
	}
	m.api.requests.Add(ctx, 1,
		observabilityx.String("route", route),
		observabilityx.String("method", method),
		observabilityx.String("status", strconv.Itoa(status)),
		observabilityx.String("result", result),
	)
	m.api.requestDuration.Record(ctx, duration.Seconds(), labels...)
	if size >= 0 {
		m.api.responseBytes.Record(ctx, float64(size), labels...)
	}
}

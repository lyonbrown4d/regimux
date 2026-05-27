package api

import (
	"context"
	"net/http"

	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

type MetricsEndpoint struct {
	metrics *observability.Metrics
}

func NewMetricsEndpoint(metrics *observability.Metrics) *MetricsEndpoint {
	return &MetricsEndpoint{metrics: metrics}
}

func (e *MetricsEndpoint) EndpointSpec() httpx.EndpointSpec {
	return endpointSpec("metrics")
}

func (e *MetricsEndpoint) Register(registrar httpx.Registrar) {
	group := registrar.Scope()
	httpx.MustGroupGet(group, "metrics", e.metricsText)
}

func (e *MetricsEndpoint) metricsText(context.Context, *struct{}) (*metricsOutput, error) {
	if e == nil || e.metrics == nil {
		return &metricsOutput{
			Status:      http.StatusServiceUnavailable,
			ContentType: distribution.MediaTypeJSON,
			Body:        streamWithStatus(http.StatusServiceUnavailable, httpx.StreamBytes([]byte(`{"error":"metrics unavailable"}`))),
		}, nil
	}
	body, err := e.metrics.Text()
	if err != nil {
		return nil, oops.Wrapf(err, "render metrics")
	}
	return &metricsOutput{
		Status:      http.StatusOK,
		ContentType: metricsContentType,
		Body:        streamWithStatus(http.StatusOK, httpx.StreamBytes(body)),
	}, nil
}

type metricsOutput struct {
	Status      int
	ContentType string `header:"Content-Type"`
	Body        httpx.ResponseStream
}

var (
	_ httpx.Endpoint             = (*MetricsEndpoint)(nil)
	_ httpx.EndpointSpecProvider = (*MetricsEndpoint)(nil)
)

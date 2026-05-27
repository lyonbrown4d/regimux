package api

import (
	"context"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/reference"
)

func (e *RegistryEndpoint) SetMetrics(metrics *observability.Metrics) {
	if e == nil {
		return
	}
	e.metrics = metrics
}

func (e *RegistryEndpoint) observeAPI(ctx context.Context, route, method string, out *registryOutput, duration time.Duration, err error) {
	if e == nil || e.metrics == nil {
		return
	}
	status := http.StatusInternalServerError
	if out != nil && out.Status != 0 {
		status = out.Status
	}
	e.metrics.ObserveAPIRequest(ctx, route, method, status, duration, err)
}

func registryRouteName(kind reference.RouteKind) string {
	if kind == "" {
		return "registry.unknown"
	}
	return "registry." + string(kind)
}

package container

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func (e *RegistryEndpoint) SetMetrics(metrics *observability.Metrics) {
	if e == nil {
		return
	}
	e.metrics = metrics
}

func (e *RegistryEndpoint) observeAPI(ctx context.Context, route, method string, out *registryOutput, duration time.Duration, err error) {
	if e == nil {
		return
	}
	status := registryOutputStatus(out)
	e.logAPIObservation(ctx, route, method, status, duration, err)
	if e.metrics != nil {
		e.metrics.ObserveAPIRequest(ctx, route, method, status, duration, registryOutputSize(out), err)
	}
}

func registryOutputStatus(out *registryOutput) int {
	if out != nil && out.Status != 0 {
		return out.Status
	}
	return http.StatusInternalServerError
}

func registryOutputSize(out *registryOutput) int64 {
	if out == nil || out.ContentLength == "" {
		return -1
	}
	size, err := strconv.ParseInt(out.ContentLength, 10, 64)
	if err != nil {
		return -1
	}
	return size
}

func (e *RegistryEndpoint) logAPIObservation(
	ctx context.Context,
	route string,
	method string,
	status int,
	duration time.Duration,
	err error,
) {
	if e.logger == nil {
		return
	}
	args := []any{"route", route, "method", method, "status", status, "duration", duration}
	if err != nil {
		e.logger.WarnContext(ctx, "registry request completed with error", append(args, "error", err)...)
		return
	}
	if status >= http.StatusInternalServerError {
		e.logger.WarnContext(ctx, "registry request completed with error", args...)
		return
	}
	e.logger.DebugContext(ctx, "registry request completed", args...)
}

func registryRouteName(kind reference.RouteKind) string {
	if kind == "" {
		return "registry.unknown"
	}
	return "registry." + string(kind)
}

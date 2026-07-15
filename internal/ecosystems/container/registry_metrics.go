package container

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/observability"
)

func (e *RegistryEndpoint) SetMetrics(metrics *observability.Metrics) {
	if e == nil {
		return
	}
	e.metrics = metrics
}

func (e *RegistryEndpoint) observeAPI(
	ctx context.Context,
	routeName string,
	route reference.Route,
	method string,
	out *registryOutput,
	duration time.Duration,
	err error,
) {
	if e == nil {
		return
	}
	status := registryOutputStatus(out)
	responseSize := registryOutputSize(out)
	e.logAPIObservation(ctx, registryAPIObservation{
		route:        routeName,
		method:       method,
		status:       status,
		duration:     duration,
		cacheStatus:  registryOutputCacheStatus(out),
		alias:        route.Alias,
		repository:   route.Repo,
		reference:    route.Reference,
		digest:       registryObservationDigest(route, out),
		responseSize: responseSize,
	}, err)
	if e.metrics != nil {
		e.metrics.ObserveAPIRequest(ctx, observability.APIRequestMetric{
			Route:    routeName,
			Method:   method,
			Status:   status,
			Duration: duration,
			Size:     responseSize,
			Err:      err,
		})
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

func registryOutputCacheStatus(out *registryOutput) string {
	if out == nil {
		return ""
	}
	return out.XMirrorCache
}

func registryObservationDigest(route reference.Route, out *registryOutput) string {
	if out != nil && out.DockerContentDigest != "" {
		return out.DockerContentDigest
	}
	if route.Digest != "" {
		return route.Digest
	}
	if reference.IsDigest(route.Reference) {
		return route.Reference
	}
	return ""
}

type registryAPIObservation struct {
	route        string
	method       string
	status       int
	duration     time.Duration
	cacheStatus  string
	alias        string
	repository   string
	reference    string
	digest       string
	responseSize int64
}

func (e *RegistryEndpoint) logAPIObservation(
	ctx context.Context,
	obs registryAPIObservation,
	err error,
) {
	if e.logger == nil {
		return
	}
	args := []any{
		"route", obs.route,
		"method", obs.method,
		"status", obs.status,
		"duration", obs.duration,
		"cache_status", obs.cacheStatus,
		"alias", obs.alias,
		"repository", obs.repository,
		"reference", obs.reference,
		"digest", obs.digest,
		"response_size", obs.responseSize,
	}
	if err != nil {
		e.logger.WarnContext(ctx, "registry request completed with error", append(args, "error", err)...)
		return
	}
	if obs.status >= http.StatusInternalServerError {
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

func (e *RegistryEndpoint) publishWorkerFillSkipped(ctx context.Context, route reference.Route, kind, reason string) {
	if e == nil || e.events == nil {
		return
	}
	status := "skipped"
	if strings.Contains(strings.ToLower(reason), "saturated") {
		status = "saturated"
	}
	if err := events.Publish(ctx, e.events, events.ContainerPullFill{
		Alias:  route.Alias,
		Source: "worker",
		Kind:   kind,
		Status: status,
		Reason: normalizeContainerPullMetricReason(reason),
	}); err != nil && e.logger != nil {
		e.logger.DebugContext(ctx, "publish container pull fill event failed", "error", err)
	}
}

func normalizeContainerPullMetricReason(reason string) string {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return "unknown"
	case strings.HasPrefix(reason, "failure backoff until "):
		return "failure_backoff"
	}
	reason = strings.ToLower(reason)
	replacer := strings.NewReplacer(
		" ", "_",
		"-", "_",
		".", "_",
		"/", "_",
		":", "_",
	)
	reason = replacer.Replace(reason)
	out := make([]rune, 0, len(reason))
	lastUnderscore := false
	for _, r := range reason {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			out = append(out, r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			out = append(out, '_')
			lastUnderscore = true
		}
	}
	normalized := strings.Trim(string(out), "_")
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

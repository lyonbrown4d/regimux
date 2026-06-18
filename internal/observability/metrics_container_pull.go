package observability

import (
	"context"
	"strings"

	"github.com/arcgolabs/observabilityx"
)

type containerPullMetrics struct {
	cacheAccesses   observabilityx.Counter
	streamFallbacks observabilityx.Counter
	fills           observabilityx.Counter
}

func newContainerPullMetrics(obs observabilityx.Observability) containerPullMetrics {
	return containerPullMetrics{
		cacheAccesses: obs.Counter(counterSpec(
			"container_pull_cache_accesses_total",
			"Total container pull cache accesses.",
			"alias", "kind", "cache_status",
		)),
		streamFallbacks: obs.Counter(counterSpec(
			"container_pull_stream_cache_fallbacks_total",
			"Total container blob stream cache fallback events.",
			"alias", "reason",
		)),
		fills: obs.Counter(counterSpec(
			"container_pull_fills_total",
			"Total container pull background fill events.",
			"alias", "source", "kind", "status", "reason",
		)),
	}
}

func (m *Metrics) ObserveContainerPullCacheAccess(ctx context.Context, event ContainerPullCacheAccessMetric) {
	if m == nil {
		return
	}
	m.container.cacheAccesses.Add(ctx, 1,
		observabilityx.String("alias", labelOrUnknown(event.Alias)),
		observabilityx.String("kind", containerPullKindLabel(event.Kind)),
		observabilityx.String("cache_status", labelOrUnknown(event.CacheStatus)),
	)
}

func (m *Metrics) ObserveContainerPullStreamCacheFallback(ctx context.Context, event ContainerPullStreamCacheFallbackMetric) {
	if m == nil {
		return
	}
	m.container.streamFallbacks.Add(ctx, 1,
		observabilityx.String("alias", labelOrUnknown(event.Alias)),
		observabilityx.String("reason", containerPullReasonLabel(event.Reason)),
	)
}

func (m *Metrics) ObserveContainerPullFill(ctx context.Context, event ContainerPullFillMetric) {
	if m == nil {
		return
	}
	m.container.fills.Add(ctx, 1,
		observabilityx.String("alias", labelOrUnknown(event.Alias)),
		observabilityx.String("source", containerPullSourceLabel(event.Source)),
		observabilityx.String("kind", containerPullKindLabel(event.Kind)),
		observabilityx.String("status", containerPullFillStatusLabel(event.Status)),
		observabilityx.String("reason", containerPullReasonLabel(event.Reason)),
	)
}

type ContainerPullCacheAccessMetric struct {
	Alias       string
	Kind        string
	CacheStatus string
}

type ContainerPullStreamCacheFallbackMetric struct {
	Alias  string
	Reason string
}

type ContainerPullFillMetric struct {
	Alias  string
	Source string
	Kind   string
	Status string
	Reason string
}

func containerPullKindLabel(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "manifest", "blob", "tags", "referrers":
		return value
	default:
		return labelOrUnknown(value)
	}
}

func containerPullSourceLabel(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "stream_cache", "worker", "prefetch":
		return value
	default:
		return labelOrUnknown(value)
	}
}

func containerPullFillStatusLabel(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case "skipped", "saturated", "failed":
		return value
	default:
		return labelOrUnknown(value)
	}
}

func containerPullReasonLabel(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return "unknown"
	case strings.HasPrefix(value, "failure backoff until "):
		return "failure_backoff"
	}
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(
		" ", "_",
		"-", "_",
		".", "_",
		"/", "_",
		":", "_",
	)
	value = replacer.Replace(value)
	out := make([]rune, 0, len(value))
	lastUnderscore := false
	for _, r := range value {
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

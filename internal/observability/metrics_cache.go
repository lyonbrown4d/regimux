package observability

import (
	"context"

	"github.com/arcgolabs/observabilityx"
)

type cacheMetrics struct {
	accesses     observabilityx.Counter
	storeBytes   observabilityx.Counter
	storeObjects observabilityx.Counter
	storeSize    observabilityx.Histogram
}

func newCacheMetrics(obs observabilityx.Observability) cacheMetrics {
	return cacheMetrics{
		accesses: obs.Counter(counterSpec(
			"cache_accesses_total",
			"Total cache accesses.",
			"kind", "alias", "repository", "status",
		)),
		storeBytes: obs.Counter(counterSpec(
			"cache_store_bytes_total",
			"Total bytes stored in cache.",
			"kind", "alias", "repository",
		)),
		storeObjects: obs.Counter(counterSpec(
			"cache_store_objects_total",
			"Total objects stored in cache.",
			"kind", "alias", "repository",
		)),
		storeSize: obs.Histogram(bytesHistogramSpec(
			"cache_store_size_bytes",
			"Cache stored object size in bytes.",
			"kind", "alias",
		)),
	}
}

type CacheAccessMetric struct {
	Kind       string
	Alias      string
	Repository string
	Status     string
}

type CacheStoreMetric struct {
	Kind       string
	Alias      string
	Repository string
	Size       int64
}

func (m *Metrics) ObserveCacheAccess(ctx context.Context, metric CacheAccessMetric) {
	kind := metric.Kind
	alias := metric.Alias
	repository := metric.Repository
	status := metric.Status
	if m == nil {
		return
	}
	m.cache.accesses.Add(ctx, 1,
		observabilityx.String("kind", kind),
		observabilityx.String("alias", alias),
		observabilityx.String("repository", repository),
		observabilityx.String("status", status),
	)
}

func (m *Metrics) ObserveCacheStore(ctx context.Context, metric CacheStoreMetric) {
	kind := metric.Kind
	alias := metric.Alias
	repository := metric.Repository
	size := metric.Size
	if m == nil {
		return
	}
	if size < 0 {
		size = 0
	}
	m.cache.storeBytes.Add(ctx, size, cacheStoreLabels(kind, alias, repository)...)
	m.cache.storeObjects.Add(ctx, 1, cacheStoreLabels(kind, alias, repository)...)
	m.cache.storeSize.Record(ctx, float64(size),
		observabilityx.String("kind", kind),
		observabilityx.String("alias", alias),
	)
}

func cacheStoreLabels(kind, alias, repository string) []observabilityx.Attribute {
	return []observabilityx.Attribute{
		observabilityx.String("kind", kind),
		observabilityx.String("alias", alias),
		observabilityx.String("repository", repository),
	}
}

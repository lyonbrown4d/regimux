package observability

import (
	"context"
	"sync"

	"github.com/arcgolabs/observabilityx"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type objectDiskCacheMetrics struct {
	mu       sync.Mutex
	stats    func() object.DiskCacheStats
	accesses observabilityx.Counter
	bytes    observabilityx.Gauge
	reserved observabilityx.Gauge
	entries  observabilityx.Gauge
	fills    observabilityx.Gauge
}

func newObjectDiskCacheMetrics(obs observabilityx.Observability) objectDiskCacheMetrics {
	return objectDiskCacheMetrics{
		accesses: obs.Counter(counterSpec("object_disk_cache_accesses_total", "Object disk cache outcomes.", "operation", "result")),
		bytes:    obs.Gauge(gaugeSpec("object_disk_cache_bytes", "Bytes occupied by cached objects.")),
		reserved: obs.Gauge(gaugeSpec("object_disk_cache_reserved_bytes", "Bytes reserved for cache fills.")),
		entries:  obs.Gauge(gaugeSpec("object_disk_cache_entries", "Complete cached objects.")),
		fills:    obs.Gauge(gaugeSpec("object_disk_cache_fills", "Active cache fills.")),
	}
}

// ObserveObjectDiskCache records bounded cache outcomes without object keys.
func (m *Metrics) ObserveObjectDiskCache(ctx context.Context, event object.DiskCacheEvent) {
	if m == nil {
		return
	}
	m.diskCache.accesses.Add(ctx, 1,
		observabilityx.String("operation", diskCacheOperation(event.Operation)),
		observabilityx.String("result", diskCacheResult(event.Result)))
}

// RegisterObjectDiskCacheStats supplies a current resource snapshot at scrape time.
func (m *Metrics) RegisterObjectDiskCacheStats(stats func() object.DiskCacheStats) {
	if m == nil {
		return
	}
	m.diskCache.mu.Lock()
	m.diskCache.stats = stats
	m.diskCache.mu.Unlock()
}

func (m *Metrics) recordObjectDiskCacheStats(ctx context.Context) {
	m.diskCache.mu.Lock()
	defer m.diskCache.mu.Unlock()
	if m.diskCache.stats == nil {
		return
	}
	stats := m.diskCache.stats()
	m.diskCache.bytes.Set(ctx, float64(stats.Bytes))
	m.diskCache.reserved.Set(ctx, float64(stats.ReservedBytes))
	m.diskCache.entries.Set(ctx, float64(stats.Entries))
	m.diskCache.fills.Set(ctx, float64(stats.Fills))
}

func diskCacheOperation(value string) string {
	switch value {
	case "get", "fill", "evict", "delete", "bypass", "restore":
		return value
	default:
		return "other"
	}
}

func diskCacheResult(value string) string {
	switch value {
	case "hit", "miss", "success", "error", "expired", "capacity", "corrupt", "canceled", "oversize", "closed", "admission":
		return value
	default:
		return "other"
	}
}

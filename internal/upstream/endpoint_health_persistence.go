package upstream

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

const endpointHealthHotStoreTimeout = 2 * time.Second

func (c *Client) LoadEndpointHealth(ctx context.Context) error {
	if c == nil || c.upstreams == nil {
		return nil
	}
	loaded := 0
	if c.metadata != nil {
		records, err := c.metadata.ListEndpointHealth(ctx)
		if err != nil {
			return oops.In("upstream").Wrapf(err, "load endpoint health metadata")
		}
		loaded += c.restoreEndpointHealthRecords(collectionlist.NewList(records...))
	}
	loaded += c.loadHotEndpointHealth(ctx)

	if loaded > 0 && c.logger != nil {
		c.logger.InfoContext(ctx, "loaded upstream endpoint health snapshots", "records", loaded)
	}
	return nil
}

func (c *Client) loadHotEndpointHealth(ctx context.Context) int {
	if !c.canLoadHotEndpointHealth() {
		return 0
	}

	aliases := c.endpointHealthAliases()
	if aliases == nil || aliases.Len() == 0 {
		return 0
	}
	return c.loadHotEndpointHealthAliases(ctx, aliases)
}

func (c *Client) canLoadHotEndpointHealth() bool {
	return c != nil &&
		c.hotHealth != nil &&
		c.upstreams != nil &&
		c.upstreams.Len() > 0
}

func (c *Client) loadHotEndpointHealthAliases(ctx context.Context, aliases *collectionlist.List[string]) int {
	records, err := c.hotHealth.List(ctx, aliases.Values()...)
	if err != nil {
		if c.logger != nil {
			c.logger.DebugContext(ctx, "load upstream endpoint health hot state failed", "error", err)
		}
		return 0
	}

	loaded := c.restoreEndpointHealthRecords(records)
	if loaded > 0 && c.logger != nil {
		c.logger.DebugContext(ctx, "loaded upstream endpoint health hot state", "records", loaded)
	}
	return loaded
}

func (c *Client) endpointHealthAliases() *collectionlist.List[string] {
	if c == nil || c.upstreams == nil {
		return collectionlist.NewList[string]()
	}
	aliases := collectionlist.NewListWithCapacity[string](c.upstreams.Len())
	c.upstreams.Range(func(alias string, _ *upstreamPool) bool {
		aliases.Add(alias)
		return true
	})
	return aliases
}

func (c *Client) restoreEndpointHealthRecords(records *collectionlist.List[meta.EndpointHealthRecord]) int {
	loaded := 0
	if records == nil {
		return 0
	}
	records.Range(func(_ int, record meta.EndpointHealthRecord) bool {
		pool, ok := c.upstreams.Get(record.Alias)
		if !ok || pool == nil || !pool.hasRegistry(record.Registry) {
			return true
		}
		pool.health.RestoreSnapshot(endpointHealthSnapshotFromRecord(&record))
		loaded++
		return true
	})
	return loaded
}

func (c *Client) recordProbeSuccess(ctx context.Context, pool *upstreamPool, runtime upstreamRuntime, latency time.Duration) {
	now := time.Now()
	snapshot := pool.recordProbeSuccess(runtime, latency, now)
	c.persistEndpointHealthSnapshot(ctx, pool.alias, snapshot)
}

func (c *Client) recordProbeFailure(ctx context.Context, pool *upstreamPool, runtime upstreamRuntime) {
	now := time.Now()
	snapshot := pool.recordProbeFailure(runtime, now)
	c.persistEndpointHealthSnapshot(ctx, pool.alias, snapshot)
}

func (c *Client) recordEndpointSuccess(ctx context.Context, req failoverRequest, pool *upstreamPool, runtime upstreamRuntime) {
	if pool == nil {
		return
	}
	now := time.Now()
	snapshot := pool.recordRequestSuccess(runtime, req.repository, now)
	c.persistEndpointHealthSnapshot(ctx, pool.alias, pool.health.Snapshot(runtime.config.Registry, now))
	if snapshot.Repository != "" {
		c.persistEndpointHealthSnapshot(ctx, pool.alias, snapshot)
	}
}

func (c *Client) recordEndpointFailure(ctx context.Context, req failoverRequest, pool *upstreamPool, runtime upstreamRuntime, err error) {
	if pool == nil {
		return
	}
	now := time.Now()
	var snapshot EndpointHealthSnapshot
	if isContentInconsistent(err) {
		snapshot = pool.recordContentInconsistent(runtime, req.repository, now)
	} else {
		snapshot = pool.recordRequestFailure(runtime, req.repository, now)
	}
	c.persistEndpointHealthSnapshot(ctx, pool.alias, pool.health.Snapshot(runtime.config.Registry, now))
	if snapshot.Repository != "" {
		c.persistEndpointHealthSnapshot(ctx, pool.alias, snapshot)
	}
}

func (c *Client) persistEndpointHealthSnapshot(ctx context.Context, alias string, snapshot EndpointHealthSnapshot) {
	if c == nil || snapshot.Registry == "" {
		return
	}
	record := endpointHealthRecordFromSnapshot(alias, snapshot)
	if c.metadata != nil {
		c.enqueueEndpointHealth(record)
	}
	c.putEndpointHealthHot(ctx, record)
}

func (c *Client) putEndpointHealthHot(ctx context.Context, record meta.EndpointHealthRecord) {
	if c == nil || c.hotHealth == nil {
		return
	}
	ctx, cancel := endpointHealthHotContext(ctx)
	defer cancel()
	if err := c.hotHealth.Put(ctx, record); err != nil && c.logger != nil {
		c.logger.DebugContext(ctx, "write upstream endpoint health hot state failed", "alias", record.Alias, "registry", record.Registry, "repository", record.Repository, "error", err)
	}
}

func endpointHealthHotContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), endpointHealthHotStoreTimeout)
}

func (p *upstreamPool) hasRegistry(registry string) bool {
	if p == nil || p.runtimes == nil {
		return false
	}
	registry = normalizeEndpointHealthRegistry(registry)
	var found bool
	p.runtimes.Range(func(_ int, runtime upstreamRuntime) bool {
		if normalizeEndpointHealthRegistry(runtime.config.Registry) == registry {
			found = true
			return false
		}
		return true
	})
	return found
}

func endpointHealthRecordFromSnapshot(alias string, snapshot EndpointHealthSnapshot) meta.EndpointHealthRecord {
	return meta.EndpointHealthRecord{
		Alias:                alias,
		Registry:             snapshot.Registry,
		Repository:           snapshot.Repository,
		LatencyEWMA:          snapshot.LatencyEWMA,
		LatencySamples:       snapshot.LatencySamples,
		ConsecutiveFailures:  snapshot.ConsecutiveFailures,
		SuccessCount:         snapshot.SuccessCount,
		FailureCount:         snapshot.FailureCount,
		ContentMismatchCount: snapshot.ContentMismatchCount,
		CooldownUntil:        snapshot.CooldownUntil,
		DegradedUntil:        snapshot.DegradedUntil,
		LastSuccessAt:        snapshot.LastSuccessAt,
		LastFailureAt:        snapshot.LastFailureAt,
		LastProbeAt:          snapshot.LastProbeAt,
	}
}

func endpointHealthSnapshotFromRecord(record *meta.EndpointHealthRecord) EndpointHealthSnapshot {
	return EndpointHealthSnapshot{
		Registry:             record.Registry,
		Repository:           record.Repository,
		LatencyEWMA:          record.LatencyEWMA,
		LatencySamples:       record.LatencySamples,
		HasLatency:           record.LatencySamples > 0,
		ConsecutiveFailures:  record.ConsecutiveFailures,
		CooldownUntil:        record.CooldownUntil,
		DegradedUntil:        record.DegradedUntil,
		LastSuccessAt:        record.LastSuccessAt,
		LastFailureAt:        record.LastFailureAt,
		LastProbeAt:          record.LastProbeAt,
		SuccessCount:         record.SuccessCount,
		FailureCount:         record.FailureCount,
		ContentMismatchCount: record.ContentMismatchCount,
	}
}

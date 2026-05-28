package upstream

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

func (c *Client) LoadEndpointHealth(ctx context.Context) error {
	if c == nil || c.metadata == nil || c.upstreams == nil {
		return nil
	}
	records, err := c.metadata.ListEndpointHealth(ctx)
	if err != nil {
		return oops.In("upstream").Wrapf(err, "load endpoint health metadata")
	}

	loaded := c.restoreEndpointHealthRecords(records)
	if loaded > 0 && c.logger != nil {
		c.logger.InfoContext(ctx, "loaded upstream endpoint health snapshots", "records", loaded)
	}
	return nil
}

func (c *Client) restoreEndpointHealthRecords(records []meta.EndpointHealthRecord) int {
	loaded := 0
	for i := range records {
		record := &records[i]
		pool, ok := c.upstreams.Get(record.Alias)
		if !ok || pool == nil || !pool.hasRegistry(record.Registry) {
			continue
		}
		pool.health.RestoreSnapshot(endpointHealthSnapshotFromRecord(record))
		loaded++
	}
	return loaded
}

func (c *Client) recordProbeSuccess(pool *upstreamPool, runtime upstreamRuntime, latency time.Duration) {
	now := time.Now()
	snapshot := pool.recordProbeSuccess(runtime, latency, now)
	c.persistEndpointHealthSnapshot(pool.alias, snapshot)
}

func (c *Client) recordProbeFailure(pool *upstreamPool, runtime upstreamRuntime) {
	now := time.Now()
	snapshot := pool.recordProbeFailure(runtime, now)
	c.persistEndpointHealthSnapshot(pool.alias, snapshot)
}

func (c *Client) recordEndpointSuccess(req failoverRequest, pool *upstreamPool, runtime upstreamRuntime) {
	if pool == nil {
		return
	}
	now := time.Now()
	snapshot := pool.recordRequestSuccess(runtime, req.repository, now)
	c.persistEndpointHealthSnapshot(pool.alias, pool.health.Snapshot(runtime.config.Registry, now))
	if snapshot.Repository != "" {
		c.persistEndpointHealthSnapshot(pool.alias, snapshot)
	}
}

func (c *Client) recordEndpointFailure(req failoverRequest, pool *upstreamPool, runtime upstreamRuntime, err error) {
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
	c.persistEndpointHealthSnapshot(pool.alias, pool.health.Snapshot(runtime.config.Registry, now))
	if snapshot.Repository != "" {
		c.persistEndpointHealthSnapshot(pool.alias, snapshot)
	}
}

func (c *Client) persistEndpointHealthSnapshot(alias string, snapshot EndpointHealthSnapshot) {
	if c == nil || c.metadata == nil || snapshot.Registry == "" {
		return
	}
	c.enqueueEndpointHealth(endpointHealthRecordFromSnapshot(alias, snapshot))
}

func (p *upstreamPool) hasRegistry(registry string) bool {
	if p == nil {
		return false
	}
	registry = normalizeEndpointHealthRegistry(registry)
	for i := range p.runtimes {
		runtime := &p.runtimes[i]
		if normalizeEndpointHealthRegistry(runtime.config.Registry) == registry {
			return true
		}
	}
	return false
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

package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
)

func upstreamMetadataMap(records *collectionlist.List[meta.Upstream]) *collectionmapping.Map[string, meta.Upstream] {
	if records == nil {
		return collectionmapping.NewMapWithCapacity[string, meta.Upstream](0)
	}
	return collectionmapping.AssociateList(
		records,
		func(_ int, row meta.Upstream) (string, meta.Upstream) {
			return row.Alias, row
		},
	)
}

func upstreamSnapshotMap(records *collectionlist.List[upstream.UpstreamSnapshot]) *collectionmapping.Map[string, upstream.UpstreamSnapshot] {
	if records == nil {
		return collectionmapping.NewMapWithCapacity[string, upstream.UpstreamSnapshot](0)
	}
	return collectionmapping.AssociateList(
		records,
		func(_ int, row upstream.UpstreamSnapshot) (string, upstream.UpstreamSnapshot) {
			return row.Alias, row
		},
	)
}

func endpointRows(snapshot upstream.UpstreamSnapshot) *collectionlist.List[EndpointRow] {
	return collectionlist.MapList(snapshot.Endpoints, func(_ int, endpoint upstream.EndpointSnapshot) EndpointRow {
		health := endpoint.Health
		return EndpointRow{
			Registry:      endpoint.Registry,
			Role:          endpoint.Role,
			Latency:       formatLatency(health),
			Score:         formatDuration(health.Score),
			Inflight:      health.Inflight,
			Failures:      health.ConsecutiveFailures,
			SuccessRate:   formatSuccessRate(health),
			Mismatches:    health.ContentMismatchCount,
			Cooldown:      formatCooldown(health),
			Degraded:      formatDegraded(health),
			LastSuccessAt: formatTime(health.LastSuccessAt),
			LastFailureAt: formatTime(health.LastFailureAt),
			Status:        endpointStatus(health),
		}
	})
}

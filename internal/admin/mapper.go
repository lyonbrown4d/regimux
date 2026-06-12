package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	mapperx "github.com/arcgolabs/mapper"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

type AdminMapper struct {
	mapper *mapperx.Mapper
}

func NewAdminMapper() *AdminMapper {
	return &AdminMapper{
		mapper: mapperx.New(
			mapperx.Converter(formatTime),
			mapperx.Converter(formatBytes),
			mapperx.Converter(metadataCount),
			mapperx.AfterMap(func(record meta.BlobRecord, row *BlobRow) {
				row.MediaType = dash(record.MediaType)
			}),
			mapperx.AfterMap(func(record meta.RepoBlobRecord, row *RepoBlobRow) {
				row.SourceManifest = dash(record.SourceManifest)
			}),
			mapperx.AfterMap(func(record meta.Repository, row *RepositoryRow) {
				row.Repository = record.Name
			}),
			mapperx.AfterMap(func(record meta.PullRecord, row *ActivityRow) {
				occurredAt := latestTime(record.LastPullAt, record.LastUpstreamPullAt, record.LastPolicyDeniedAt, record.UpdatedAt, record.CreatedAt)
				row.OccurredAt = formatTime(occurredAt)
				row.Event = "pull"
				row.Actor = "-"
				row.Method = "-"
				row.Path = "-"
				row.UpstreamAt = formatTime(record.LastUpstreamPullAt)
				row.PolicyDeniedAt = formatTime(record.LastPolicyDeniedAt)
				row.Source = "meta.pull_records"
				row.RequestID = "-"
			}),
			mapperx.AfterMap(func(endpoint ecosystem.EndpointSnapshot, row *EndpointRow) {
				health := endpoint.Health
				row.Latency = formatLatency(health)
				row.Score = formatDuration(health.Score)
				row.Inflight = health.Inflight
				row.Failures = health.ConsecutiveFailures
				row.SuccessRate = formatSuccessRate(health)
				row.Mismatches = health.ContentMismatchCount
				row.Cooldown = formatCooldown(health)
				row.Degraded = formatDegraded(health)
				row.LastSuccessAt = formatTime(health.LastSuccessAt)
				row.LastFailureAt = formatTime(health.LastFailureAt)
				row.Status = endpointStatus(health)
			}),
		),
	}
}

func (m *AdminMapper) PullRows(records *collectionlist.List[meta.PullRecord]) (*collectionlist.List[PullRow], error) {
	return mapAdminList(records, 0, m.PullRow)
}

func (m *AdminMapper) PullRow(record meta.PullRecord) (PullRow, error) {
	return mapAdmin[PullRow](m, record, "map admin pull row")
}

func (m *AdminMapper) ActivityRowsFromPulls(records *collectionlist.List[meta.PullRecord], limit int) (*collectionlist.List[ActivityRow], error) {
	return mapAdminList(records, limit, m.ActivityRowFromPull)
}

func (m *AdminMapper) ActivityRowFromPull(record meta.PullRecord) (ActivityRow, error) {
	return mapAdmin[ActivityRow](m, record, "map admin activity row")
}

func (m *AdminMapper) BlobRows(records *collectionlist.List[meta.BlobRecord], limit int) (*collectionlist.List[BlobRow], error) {
	return mapAdminList(records, limit, m.BlobRow)
}

func (m *AdminMapper) BlobRow(record meta.BlobRecord) (BlobRow, error) {
	return mapAdmin[BlobRow](m, record, "map admin blob row")
}

func (m *AdminMapper) RepoBlobRows(records *collectionlist.List[meta.RepoBlobRecord], limit int) (*collectionlist.List[RepoBlobRow], error) {
	return mapAdminList(records, limit, m.RepoBlobRow)
}

func (m *AdminMapper) RepoBlobRow(record meta.RepoBlobRecord) (RepoBlobRow, error) {
	return mapAdmin[RepoBlobRow](m, record, "map admin repo blob row")
}

func (m *AdminMapper) RepositoryRows(records *collectionlist.List[meta.Repository], limit int) (*collectionlist.List[RepositoryRow], error) {
	return mapAdminList(records, limit, m.RepositoryRow)
}

func (m *AdminMapper) RepositoryRow(record meta.Repository) (RepositoryRow, error) {
	return mapAdmin[RepositoryRow](m, record, "map admin repository row")
}

func (m *AdminMapper) EndpointRows(snapshot ecosystem.UpstreamSnapshot) (*collectionlist.List[EndpointRow], error) {
	return mapAdminList(snapshot.Endpoints, 0, m.EndpointRow)
}

func (m *AdminMapper) EndpointRow(snapshot ecosystem.EndpointSnapshot) (EndpointRow, error) {
	return mapAdmin[EndpointRow](m, snapshot, "map admin endpoint row")
}

func (m *AdminMapper) ApplyUpstreamMetadata(row *UpstreamRow, record meta.Upstream) error {
	if row == nil {
		return nil
	}
	return mapAdminInto(m, row, record, "map admin upstream metadata")
}

func mapAdmin[D any](m *AdminMapper, src any, message string) (D, error) {
	var dst D
	if err := mapAdminInto(m, &dst, src, message); err != nil {
		return dst, err
	}
	return dst, nil
}

func mapAdminInto(m *AdminMapper, dst, src any, message string) error {
	if m == nil || m.mapper == nil {
		m = NewAdminMapper()
	}
	if err := m.mapper.MapInto(dst, src); err != nil {
		return oops.In("admin").With("mapping", message).Wrapf(err, "map admin row")
	}
	return nil
}

func mapAdminList[T, R any](
	records *collectionlist.List[T],
	limit int,
	mapOne func(T) (R, error),
) (*collectionlist.List[R], error) {
	out := collectionlist.NewList[R]()
	if records == nil {
		return out, nil
	}
	source := records
	if limit > 0 {
		source = records.Take(limit)
	}
	mapped, err := collectionlist.ReduceErrList(source, out, func(out *collectionlist.List[R], _ int, record T) (*collectionlist.List[R], error) {
		row, err := mapOne(record)
		if err != nil {
			return nil, err
		}
		out.Add(row)
		return out, nil
	})
	if err != nil {
		return nil, oops.In("admin").Wrapf(err, "map admin list")
	}
	return mapped, nil
}

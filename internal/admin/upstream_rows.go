package admin

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) upstreamRows(now time.Time, metadata *collectionlist.List[meta.Upstream]) (*collectionlist.List[UpstreamRow], error) {
	snapshots := collectUpstreamSnapshots(s.runtimes, now)
	stats := upstreamMetadataMap(metadata)

	rows := collectionlist.NewList[UpstreamRow]()
	if err := s.addUpstreamSnapshotRows(rows, s.configuredUpstreams(), stats, snapshots); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Service) addUpstreamSnapshotRows(
	rows *collectionlist.List[UpstreamRow],
	upstreams *collectionlist.List[ecosystem.Upstream],
	stats *mapping.Map[string, meta.Upstream],
	snapshots *mapping.Map[string, ecosystem.UpstreamSnapshot],
) error {
	if rows == nil || upstreams == nil {
		return nil
	}
	var mapErr error
	upstreams.Range(func(_ int, upstream ecosystem.Upstream) bool {
		row := upstreamRowFromConfig(upstream.Ecosystem, upstream.Alias, upstream.Config)
		if err := s.applyUpstreamRuntimeState(&row, stats, upstream); err != nil {
			mapErr = err
			return false
		}
		runtimeSnapshot, _ := snapshots.Get(upstreamSnapshotKey(upstream.Ecosystem, upstream.Alias))
		endpoints, err := s.endpointRows(runtimeSnapshot)
		if err != nil {
			mapErr = err
			return false
		}
		row.Endpoints = endpoints
		rows.Add(row)
		return true
	})
	return mapErr
}

func upstreamRowFromConfig(ecosystemName, alias string, upstreamCfg config.UpstreamConfig) UpstreamRow {
	return UpstreamRow{
		Ecosystem:        ecosystemName,
		DisplayAlias:     upstreamDisplayAlias(ecosystemName, alias),
		Alias:            alias,
		Registry:         upstreamCfg.Registry,
		DefaultNamespace: upstreamCfg.DefaultNamespace,
		AuthType:         upstreamCfg.Auth.Type,
		MirrorPolicy:     upstreamCfg.MirrorPolicy,
		BlobPolicy:       upstreamCfg.Blob.MirrorPolicy,
		ProbeEnabled:     upstreamCfg.Probe.Enabled,
		MirrorCount:      len(upstreamCfg.Mirrors),
	}
}

func upstreamDisplayAlias(ecosystemName, alias string) string {
	if ecosystemName == "" {
		return alias
	}
	return ecosystemName + "/" + alias
}

func (s *Service) applyUpstreamRuntimeState(
	row *UpstreamRow,
	stats *mapping.Map[string, meta.Upstream],
	upstream ecosystem.Upstream,
) error {
	if stats == nil {
		return nil
	}
	runtime, ok := stats.Get(metadataAliasForUpstream(upstream))
	if !ok {
		return nil
	}
	return s.mapper.ApplyUpstreamMetadata(row, runtime)
}

func metadataAliasForUpstream(upstream ecosystem.Upstream) string {
	return ecosystem.ScopedAlias(upstream.Ecosystem, upstream.Alias)
}

func collectUpstreamSnapshots(runtimes *collectionlist.List[ecosystem.Runtime], now time.Time) *mapping.Map[string, ecosystem.UpstreamSnapshot] {
	if runtimes == nil {
		return mapping.NewMapWithCapacity[string, ecosystem.UpstreamSnapshot](0)
	}
	out := mapping.NewMapWithCapacity[string, ecosystem.UpstreamSnapshot](runtimes.Len())
	runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		provider, ok := runtime.(ecosystem.UpstreamSnapshotProvider)
		if !ok || provider == nil {
			return true
		}
		clientSnapshot := provider.Snapshot(now)
		if clientSnapshot.Upstreams == nil {
			return true
		}
		clientSnapshot.Upstreams.Range(func(_ int, upstreamSnapshot ecosystem.UpstreamSnapshot) bool {
			out.Set(upstreamSnapshotKey(upstreamSnapshot.Ecosystem, upstreamSnapshot.Alias), upstreamSnapshot)
			return true
		})
		return true
	})
	return out
}

func upstreamSnapshotKey(ecosystemName, alias string) string {
	if ecosystemName == "" {
		return alias
	}
	return ecosystemName + "\x1f" + alias
}

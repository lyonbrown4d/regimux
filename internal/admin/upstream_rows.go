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
	if err := s.addUpstreamSnapshotRows(rows, ecosystem.Container, s.cfg.OrderedContainerUpstreams(), stats, snapshots); err != nil {
		return nil, err
	}
	if err := s.addUpstreamSnapshotRows(rows, "go", s.cfg.OrderedGoUpstreams(), stats, snapshots); err != nil {
		return nil, err
	}
	if err := s.addUpstreamSnapshotRows(rows, "npm", s.cfg.OrderedNPMUpstreams(), stats, snapshots); err != nil {
		return nil, err
	}
	if err := s.addUpstreamSnapshotRows(rows, "pypi", s.cfg.OrderedPyPIUpstreams(), stats, snapshots); err != nil {
		return nil, err
	}
	if err := s.addUpstreamSnapshotRows(rows, "maven", s.cfg.OrderedMavenUpstreams(), stats, snapshots); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Service) addUpstreamSnapshotRows(
	rows *collectionlist.List[UpstreamRow],
	ecosystemName string,
	ordered *mapping.OrderedMap[string, config.UpstreamConfig],
	stats *mapping.Map[string, meta.Upstream],
	snapshots *mapping.Map[string, ecosystem.UpstreamSnapshot],
) error {
	if rows == nil || ordered == nil {
		return nil
	}
	var mapErr error
	ordered.Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		row := upstreamRowFromConfig(ecosystemName, alias, upstreamCfg)
		if err := s.applyUpstreamRuntimeState(&row, stats, alias); err != nil {
			mapErr = err
			return false
		}
		runtimeSnapshot, _ := snapshots.Get(upstreamSnapshotKey(ecosystemName, alias))
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
	alias string,
) error {
	if stats == nil {
		return nil
	}
	runtime, ok := stats.Get(alias)
	if !ok {
		return nil
	}
	return s.mapper.ApplyUpstreamMetadata(row, runtime)
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

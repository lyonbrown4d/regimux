package admin

import (
	"fmt"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) upstreamRows(now time.Time, metadata *collectionlist.List[meta.Upstream]) *collectionlist.List[UpstreamRow] {
	snapshots := collectUpstreamSnapshots(s.runtimes, now)
	stats := upstreamMetadataMap(metadata)

	rows := collectionlist.NewList[UpstreamRow]()
	addUpstreamSnapshotRows(rows, "oci", s.cfg.OrderedContainerUpstreams(), stats, snapshots)
	addUpstreamSnapshotRows(rows, "go", s.cfg.OrderedGoUpstreams(), stats, snapshots)
	addUpstreamSnapshotRows(rows, "npm", s.cfg.OrderedNPMUpstreams(), stats, snapshots)
	addUpstreamSnapshotRows(rows, "pypi", s.cfg.OrderedPyPIUpstreams(), stats, snapshots)
	addUpstreamSnapshotRows(rows, "maven", s.cfg.OrderedMavenUpstreams(), stats, snapshots)
	return rows
}

func addUpstreamSnapshotRows(
	rows *collectionlist.List[UpstreamRow],
	ecosystemName string,
	ordered *mapping.OrderedMap[string, config.UpstreamConfig],
	stats *mapping.Map[string, meta.Upstream],
	snapshots *mapping.Map[string, ecosystem.UpstreamSnapshot],
) {
	if rows == nil || ordered == nil {
		return
	}
	ordered.Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		displayAlias := alias
		if ecosystemName != "" {
			displayAlias = fmt.Sprintf("%s/%s", ecosystemName, alias)
		}
		row := UpstreamRow{
			Ecosystem:        ecosystemName,
			DisplayAlias:     displayAlias,
			Alias:            alias,
			Registry:         upstreamCfg.Registry,
			DefaultNamespace: upstreamCfg.DefaultNamespace,
			AuthType:         upstreamCfg.Auth.Type,
			MirrorPolicy:     upstreamCfg.MirrorPolicy,
			BlobPolicy:       upstreamCfg.Blob.MirrorPolicy,
			ProbeEnabled:     upstreamCfg.Probe.Enabled,
			MirrorCount:      len(upstreamCfg.Mirrors),
		}
		if runtime, ok := stats.Get(alias); ok {
			row.RepositoryCount = metadataCount(runtime.RepositoryCount)
			row.PullCount = runtime.PullCount
			row.BlobBytes = formatBytes(runtime.BlobBytes)
			row.LastActivityAt = formatTime(runtime.LastActivityAt)
		}
		runtimeSnapshot, _ := snapshots.Get(upstreamSnapshotKey(ecosystemName, alias))
		row.Endpoints = endpointRows(runtimeSnapshot)
		rows.Add(row)
		return true
	})
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

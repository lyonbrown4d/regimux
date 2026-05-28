package admin

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/samber/oops"
)

type metadataSnapshot struct {
	stats        meta.MetadataStats
	upstreams    *collectionlist.List[meta.Upstream]
	repositories *collectionlist.List[meta.Repository]
	pulls        *collectionlist.List[meta.PullRecord]
	recentBlobs  *collectionlist.List[meta.BlobRecord]
	largeBlobs   *collectionlist.List[meta.BlobRecord]
	repoBlobs    *collectionlist.List[meta.RepoBlobRecord]
}

func (s *Service) metadataRows(ctx context.Context, now time.Time, active string) (metadataSnapshot, error) {
	if s.metadata == nil {
		return newMetadataSnapshot(meta.MetadataStats{}), nil
	}

	stats, err := s.metadata.MetadataStats(ctx, now)
	if err != nil {
		return metadataSnapshot{}, oops.In("admin").Wrapf(err, "load metadata stats")
	}

	rows := newMetadataSnapshot(stats)
	if err := s.loadPullRows(ctx, active, &rows); err != nil {
		return metadataSnapshot{}, err
	}
	if err := s.loadRepositoryRows(ctx, active, &rows); err != nil {
		return metadataSnapshot{}, err
	}
	if err := s.loadBlobRows(ctx, active, &rows); err != nil {
		return metadataSnapshot{}, err
	}
	if err := s.loadUpstreamMetadataRows(ctx, active, &rows); err != nil {
		return metadataSnapshot{}, err
	}
	return rows, nil
}

func newMetadataSnapshot(stats meta.MetadataStats) metadataSnapshot {
	return metadataSnapshot{
		stats:        stats,
		upstreams:    collectionlist.NewList[meta.Upstream](),
		repositories: collectionlist.NewList[meta.Repository](),
		pulls:        collectionlist.NewList[meta.PullRecord](),
		recentBlobs:  collectionlist.NewList[meta.BlobRecord](),
		largeBlobs:   collectionlist.NewList[meta.BlobRecord](),
		repoBlobs:    collectionlist.NewList[meta.RepoBlobRecord](),
	}
}

func (s *Service) loadPullRows(ctx context.Context, active string, rows *metadataSnapshot) error {
	pullLimit := pullRowLimit(active)
	if pullLimit < 0 {
		return nil
	}
	opts := []meta.PullListOption{meta.PullListRecentFirst()}
	if pullLimit > 0 {
		opts = append(opts, meta.PullListLimit(pullLimit))
	}
	pulls, err := s.metadata.ListPulls(ctx, opts...)
	if err != nil {
		return oops.In("admin").Wrapf(err, "list pulls")
	}
	rows.pulls = collectionlist.NewList(pulls...)
	return nil
}

func (s *Service) loadBlobRows(ctx context.Context, active string, rows *metadataSnapshot) error {
	recentBlobLimit := recentBlobRowLimit(active)
	if recentBlobLimit > 0 {
		recentBlobs, err := s.metadata.ListBlobs(ctx,
			meta.BlobListOrderByRecent(),
			meta.BlobListLimit(recentBlobLimit),
		)
		if err != nil {
			return oops.In("admin").Wrapf(err, "list recent blobs")
		}
		rows.recentBlobs = collectionlist.NewList(recentBlobs...)
	}
	if active != "storage" {
		return nil
	}
	return s.loadStorageBlobRows(ctx, rows)
}

func (s *Service) loadRepositoryRows(ctx context.Context, active string, rows *metadataSnapshot) error {
	repositoryLimit := repositoryRowLimit(active)
	if repositoryLimit < 0 {
		return nil
	}
	opts := []meta.RepositoryListOption{meta.RepositoryListRecentFirst()}
	if repositoryLimit > 0 {
		opts = append(opts, meta.RepositoryListLimit(repositoryLimit))
	}
	repositories, err := s.metadata.ListRepositories(ctx, opts...)
	if err != nil {
		return oops.In("admin").Wrapf(err, "list repository metadata")
	}
	rows.repositories = collectionlist.NewList(repositories...)
	return nil
}

func (s *Service) loadUpstreamMetadataRows(ctx context.Context, active string, rows *metadataSnapshot) error {
	if active != "upstreams" && active != "dashboard" {
		return nil
	}
	upstreams, err := s.metadata.ListUpstreams(ctx, meta.UpstreamListRecentFirst())
	if err != nil {
		return oops.In("admin").Wrapf(err, "list upstream metadata")
	}
	rows.upstreams = collectionlist.NewList(upstreams...)
	return nil
}

func (s *Service) loadStorageBlobRows(ctx context.Context, rows *metadataSnapshot) error {
	largeBlobs, err := s.metadata.ListBlobs(ctx,
		meta.BlobListOrderByLargest(),
		meta.BlobListLimit(10),
	)
	if err != nil {
		return oops.In("admin").Wrapf(err, "list large blobs")
	}
	repoBlobs, err := s.metadata.ListRepoBlobs(ctx,
		meta.RepoBlobListRecentFirst(),
		meta.RepoBlobListLimit(25),
	)
	if err != nil {
		return oops.In("admin").Wrapf(err, "list repo blobs")
	}
	rows.largeBlobs = collectionlist.NewList(largeBlobs...)
	rows.repoBlobs = collectionlist.NewList(repoBlobs...)
	return nil
}

func (s *Service) summary(snapshot metadataSnapshot, upstreams *collectionlist.List[UpstreamRow], now time.Time) Summary {
	stats := snapshot.stats
	return Summary{
		Version:            string(s.version),
		Uptime:             formatDuration(now.Sub(s.startedAt)),
		Listen:             s.cfg.Server.Listen,
		PublicURL:          s.cfg.Server.PublicURL,
		AuthEnabled:        s.cfg.Auth.Enabled,
		CacheBackend:       s.cfg.Cache.Backend,
		SchedulerEnabled:   s.cfg.Scheduler.Enabled,
		DistributedLock:    s.cfg.Scheduler.DistributedLock,
		UpstreamCount:      upstreams.Len(),
		MirrorCount:        mirrorCount(upstreams),
		ManifestCount:      metadataCount(stats.ManifestCount),
		TagCount:           metadataCount(stats.TagCount),
		BlobCount:          metadataCount(stats.BlobCount),
		RepoBlobCount:      metadataCount(stats.RepoBlobCount),
		RepositoryCount:    metadataCount(stats.RepositoryCount),
		RepositoryBytes:    formatBytes(stats.RepositoryBytes),
		BlobBytes:          formatBytes(stats.BlobBytes),
		PullCount:          metadataCount(stats.PullCount),
		LastPullAt:         formatTime(stats.LastPullAt),
		LastUpstreamPullAt: formatTime(stats.LastUpstreamPullAt),
	}
}

func pullRowLimit(active string) int {
	switch active {
	case "dashboard":
		return 10
	case "activity":
		return 50
	case "pulls":
		return 0
	default:
		return -1
	}
}

func recentBlobRowLimit(active string) int {
	switch active {
	case "cache":
		return 25
	case "storage":
		return 10
	default:
		return 0
	}
}

func repositoryRowLimit(active string) int {
	switch active {
	case "dashboard":
		return 5
	case "storage":
		return 25
	default:
		return -1
	}
}

func metadataCount(value int64) int {
	if value <= 0 {
		return 0
	}
	maxInt := int64(^uint(0) >> 1)
	if value > maxInt {
		return int(maxInt)
	}
	return int(value)
}

func (s *Service) upstreamRows(now time.Time, metadata *collectionlist.List[meta.Upstream]) *collectionlist.List[UpstreamRow] {
	snapshot := upstream.ClientSnapshot{}
	if s.upstream != nil {
		snapshot = s.upstream.Snapshot(now)
	}
	snapshots := upstreamSnapshotMap(collectionlist.NewList(snapshot.Upstreams...))
	stats := upstreamMetadataMap(metadata)

	rows := collectionlist.NewListWithCapacity[UpstreamRow](len(s.cfg.Upstreams))
	s.cfg.OrderedUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		row := UpstreamRow{
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
		runtimeSnapshot, _ := snapshots.Get(alias)
		row.Endpoints = endpointRows(runtimeSnapshot).Values()
		rows.Add(row)
		return true
	})
	return rows
}

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
	return collectionlist.MapList(collectionlist.NewList(snapshot.Endpoints...), func(_ int, endpoint upstream.EndpointSnapshot) EndpointRow {
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

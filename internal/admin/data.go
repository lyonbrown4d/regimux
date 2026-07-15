package admin

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
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
	rows.pulls = pulls
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
		rows.recentBlobs = recentBlobs
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
	rows.repositories = repositories
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
	rows.upstreams = upstreams
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
	rows.largeBlobs = largeBlobs
	rows.repoBlobs = repoBlobs
	return nil
}

func (s *Service) summary(snapshot metadataSnapshot, upstreams *collectionlist.List[UpstreamRow], now time.Time) Summary {
	stats := snapshot.stats
	return Summary{
		Version:                string(s.version),
		Uptime:                 formatDuration(now.Sub(s.startedAt)),
		Listen:                 s.cfg.Server.Listen,
		PublicURL:              s.cfg.Server.PublicURL,
		AuthEnabled:            s.cfg.Auth.Enabled,
		CacheBackend:           s.cfg.Cache.Backend,
		SchedulerEnabled:       s.cfg.Scheduler.Enabled,
		DistributedLock:        s.cfg.Scheduler.DistributedLock,
		UpstreamCount:          upstreams.Len(),
		MirrorCount:            mirrorCount(upstreams),
		ManifestCount:          metadataCount(stats.ManifestCount),
		TagCount:               metadataCount(stats.TagCount),
		BlobCount:              metadataCount(stats.BlobCount),
		RepoBlobCount:          metadataCount(stats.RepoBlobCount),
		RepositoryCount:        metadataCount(stats.RepositoryCount),
		RepositoryBytes:        formatBytes(stats.RepositoryBytes),
		BlobBytes:              formatBytes(stats.BlobBytes),
		PullCount:              metadataCount(stats.PullCount),
		PolicyDeniedPullCount:  metadataCount(stats.PolicyDeniedPullCount),
		LastPullAt:             formatTime(stats.LastPullAt),
		LastUpstreamPullAt:     formatTime(stats.LastUpstreamPullAt),
		LastPolicyDeniedPullAt: formatTime(stats.LastPolicyDeniedPullAt),
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

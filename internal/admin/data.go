package admin

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/samber/oops"
)

type metadataSnapshot struct {
	manifests []meta.ManifestRecord
	tags      []meta.TagRecord
	pulls     []meta.PullRecord
	blobs     []meta.BlobRecord
	repoBlobs []meta.RepoBlobRecord
}

func (s *Service) metadataRows(ctx context.Context, _ time.Time) (metadataSnapshot, error) {
	if s.metadata == nil {
		return metadataSnapshot{}, nil
	}

	manifests, err := s.metadata.ListManifests(ctx)
	if err != nil {
		return metadataSnapshot{}, oops.In("admin").Wrapf(err, "list manifests")
	}
	tags, err := s.metadata.ListTags(ctx)
	if err != nil {
		return metadataSnapshot{}, oops.In("admin").Wrapf(err, "list tags")
	}
	pulls, err := s.metadata.ListPulls(ctx)
	if err != nil {
		return metadataSnapshot{}, oops.In("admin").Wrapf(err, "list pulls")
	}
	blobs, err := s.metadata.ListBlobs(ctx)
	if err != nil {
		return metadataSnapshot{}, oops.In("admin").Wrapf(err, "list blobs")
	}
	repoBlobs, err := s.metadata.ListRepoBlobs(ctx)
	if err != nil {
		return metadataSnapshot{}, oops.In("admin").Wrapf(err, "list repo blobs")
	}

	return metadataSnapshot{
		manifests: manifests,
		tags:      tags,
		pulls:     pulls,
		blobs:     blobs,
		repoBlobs: repoBlobs,
	}, nil
}

func (s *Service) summary(snapshot metadataSnapshot, upstreams []UpstreamRow, pulls []PullRow, now time.Time) Summary {
	lastPullAt, lastUpstreamPullAt := latestPullTimes(snapshot.pulls)
	return Summary{
		Version:            string(s.version),
		Uptime:             formatDuration(now.Sub(s.startedAt)),
		Listen:             s.cfg.Server.Listen,
		PublicURL:          s.cfg.Server.PublicURL,
		AuthEnabled:        s.cfg.Auth.Enabled,
		CacheBackend:       s.cfg.Cache.Backend,
		SchedulerEnabled:   s.cfg.Scheduler.Enabled,
		DistributedLock:    s.cfg.Scheduler.DistributedLock,
		UpstreamCount:      len(upstreams),
		MirrorCount:        mirrorCount(upstreams),
		ManifestCount:      len(snapshot.manifests),
		TagCount:           len(snapshot.tags),
		BlobCount:          len(snapshot.blobs),
		RepoBlobCount:      len(snapshot.repoBlobs),
		BlobBytes:          formatBytes(blobBytes(snapshot.blobs)),
		PullCount:          len(pulls),
		LastPullAt:         formatTime(lastPullAt),
		LastUpstreamPullAt: formatTime(lastUpstreamPullAt),
	}
}

func (s *Service) upstreamRows(now time.Time) []UpstreamRow {
	snapshot := upstream.ClientSnapshot{}
	if s.upstream != nil {
		snapshot = s.upstream.Snapshot(now)
	}
	snapshots := upstreamSnapshotMap(snapshot)

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
		row.Endpoints = endpointRows(snapshots[alias])
		rows.Add(row)
		return true
	})
	return rows.Values()
}

func upstreamSnapshotMap(snapshot upstream.ClientSnapshot) map[string]upstream.UpstreamSnapshot {
	out := make(map[string]upstream.UpstreamSnapshot, len(snapshot.Upstreams))
	for i := range snapshot.Upstreams {
		row := snapshot.Upstreams[i]
		out[row.Alias] = row
	}
	return out
}

func endpointRows(snapshot upstream.UpstreamSnapshot) []EndpointRow {
	return collectionlist.MapList(collectionlist.NewList(snapshot.Endpoints...), func(_ int, endpoint upstream.EndpointSnapshot) EndpointRow {
		health := endpoint.Health
		return EndpointRow{
			Registry:      endpoint.Registry,
			Role:          endpoint.Role,
			Latency:       formatLatency(health),
			Score:         formatDuration(health.Score),
			Inflight:      health.Inflight,
			Failures:      health.ConsecutiveFailures,
			Cooldown:      formatCooldown(health),
			LastSuccessAt: formatTime(health.LastSuccessAt),
			LastFailureAt: formatTime(health.LastFailureAt),
			Status:        endpointStatus(health),
		}
	}).Values()
}

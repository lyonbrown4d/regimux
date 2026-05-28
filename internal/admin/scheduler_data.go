package admin

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func (s *Service) schedulerSummary() SchedulerSummary {
	cfg := s.cfg.Scheduler
	return SchedulerSummary{
		Enabled:                      cfg.Enabled,
		DistributedLock:              cfg.DistributedLock,
		LockTTL:                      formatDuration(cfg.LockTTL),
		CleanupEnabled:               cfg.Cleanup.Enabled,
		CleanupInterval:              formatDuration(cfg.Cleanup.Interval),
		CleanupUnusedFor:             formatDuration(cfg.Cleanup.UnusedFor),
		CleanupMaxScan:               cfg.Cleanup.MaxScan,
		CleanupMaxDeletes:            cfg.Cleanup.MaxDeletes,
		CleanupDryRun:                cfg.Cleanup.DryRun,
		PrefetchEnabled:              cfg.Prefetch.Enabled,
		PrefetchInterval:             formatDuration(cfg.Prefetch.Interval),
		PrefetchMinPullCount:         cfg.Prefetch.MinPullCount,
		PrefetchMaxRecords:           cfg.Prefetch.MaxRecords,
		PrefetchMaxCandidatesPerRepo: cfg.Prefetch.MaxCandidatesPerRepo,
		PrefetchMaxVersionDistance:   cfg.Prefetch.MaxVersionDistance,
		ProbeJobs:                    probeJobRows(s.cfg),
	}
}

func probeJobRows(cfg config.Config) []ProbeJobRow {
	rows := collectionlist.NewListWithCapacity[ProbeJobRow](len(cfg.Upstreams))
	cfg.OrderedUpstreams().Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		rows.Add(ProbeJobRow{
			Alias:    alias,
			Enabled:  upstreamCfg.Probe.Enabled,
			Interval: formatDuration(upstreamCfg.Probe.Interval),
			Timeout:  formatDuration(upstreamCfg.Probe.Timeout),
			Cooldown: formatDuration(upstreamCfg.Probe.Cooldown),
		})
		return true
	})
	return rows.Values()
}

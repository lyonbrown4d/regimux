package admin

import (
	"context"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

func (s *Service) schedulerSummary(ctx context.Context) (SchedulerSummary, error) {
	cfg := s.cfg.Scheduler
	runs, outcomes, err := s.prefetchHistoryRows(ctx)
	if err != nil {
		return SchedulerSummary{}, err
	}
	return SchedulerSummary{
		Enabled:                      cfg.Enabled,
		DistributedLock:              cfg.DistributedLock,
		LockTTL:                      formatDuration(cfg.LockTTL),
		CleanupEnabled:               cfg.Cleanup.Enabled,
		CleanupInterval:              formatDuration(cfg.Cleanup.Interval),
		CleanupUnusedFor:             formatDuration(cfg.Cleanup.UnusedFor),
		CleanupMaxScan:               cfg.Cleanup.MaxScan,
		CleanupMaxDeletes:            cfg.Cleanup.MaxDeletes,
		CleanupMaxBytes:              formatBytes(cfg.Cleanup.MaxBytes),
		CleanupTargetBytes:           formatBytes(cfg.Cleanup.TargetBytes),
		CleanupDryRun:                cfg.Cleanup.DryRun,
		PrefetchEnabled:              cfg.Prefetch.Enabled,
		PrefetchInterval:             formatDuration(cfg.Prefetch.Interval),
		PrefetchMinPullCount:         cfg.Prefetch.MinPullCount,
		PrefetchMaxRecords:           cfg.Prefetch.MaxRecords,
		PrefetchMaxCandidatesPerRepo: cfg.Prefetch.MaxCandidatesPerRepo,
		PrefetchMaxVersionDistance:   cfg.Prefetch.MaxVersionDistance,
		PrefetchMaxBytes:             formatBytes(cfg.Prefetch.MaxBytes),
		PrefetchMaxTasks:             cfg.Prefetch.MaxTasks,
		PrefetchMaxRepositories:      cfg.Prefetch.MaxRepositories,
		PrefetchFailureBackoff:       formatDuration(cfg.Prefetch.FailureBackoff),
		PrefetchRetryWindow:          formatDuration(cfg.Prefetch.RetryWindow),
		PrefetchRuns:                 runs,
		PrefetchOutcomes:             outcomes,
		ProbeJobs:                    probeJobRows(s.configuredUpstreams()),
	}, nil
}

func probeJobRows(upstreams *collectionlist.List[ecosystem.Upstream]) *collectionlist.List[ProbeJobRow] {
	if upstreams == nil {
		return collectionlist.NewList[ProbeJobRow]()
	}
	rows := collectionlist.NewListWithCapacity[ProbeJobRow](upstreams.Len())
	upstreams.Range(func(_ int, upstream ecosystem.Upstream) bool {
		rows.Add(ProbeJobRow{
			Ecosystem: upstream.Ecosystem,
			Alias:     upstream.Alias,
			Enabled:   upstream.Config.Probe.Enabled,
			Interval:  formatDuration(upstream.Config.Probe.Interval),
			Timeout:   formatDuration(upstream.Config.Probe.Timeout),
			Cooldown:  formatDuration(upstream.Config.Probe.Cooldown),
			Jitter:    formatDuration(upstream.Config.Probe.Jitter),
		})
		return true
	})
	return rows
}

func (s *Service) prefetchHistoryRows(ctx context.Context) (*collectionlist.List[PrefetchRunRow], *collectionlist.List[PrefetchOutcomeRow], error) {
	if s == nil || s.metadata == nil {
		return collectionlist.NewList[PrefetchRunRow](), collectionlist.NewList[PrefetchOutcomeRow](), nil
	}
	runs, err := s.metadata.ListPrefetchRuns(ctx, meta.PrefetchRunListRecentFirst(), meta.PrefetchRunListLimit(10))
	if err != nil {
		return nil, nil, oops.In("admin").Wrapf(err, "list prefetch runs")
	}
	outcomes, err := s.metadata.ListPrefetchOutcomes(ctx, meta.PrefetchOutcomeListRecentFirst(), meta.PrefetchOutcomeListLimit(25))
	if err != nil {
		return nil, nil, oops.In("admin").Wrapf(err, "list prefetch outcomes")
	}
	return prefetchRunRows(collectionlist.NewList(runs...)), prefetchOutcomeRows(collectionlist.NewList(outcomes...)), nil
}

func prefetchRunRows(records *collectionlist.List[meta.PrefetchRunRecord]) *collectionlist.List[PrefetchRunRow] {
	if records == nil {
		return collectionlist.NewList[PrefetchRunRow]()
	}
	rows := collectionlist.NewListWithCapacity[PrefetchRunRow](records.Len())
	records.Range(func(_ int, record meta.PrefetchRunRecord) bool {
		rows.Add(PrefetchRunRow{
			ID:                  record.ID,
			Status:              record.Status,
			StartedAt:           formatTime(record.StartedAt),
			FinishedAt:          formatTime(record.FinishedAt),
			ScannedRecords:      record.ScannedRecords,
			Repositories:        record.Repositories,
			SkippedRepositories: record.SkippedRepositories,
			Candidates:          record.Candidates,
			Prefetched:          record.Prefetched,
			Failed:              record.Failed,
			SkippedCandidates:   record.SkippedCandidates,
			BytesWarmed:         formatBytes(record.BytesWarmed),
			RetryRequested:      record.RetryRequested,
			Error:               record.Error,
		})
		return true
	})
	return rows
}

func prefetchOutcomeRows(records *collectionlist.List[meta.PrefetchOutcomeRecord]) *collectionlist.List[PrefetchOutcomeRow] {
	if records == nil {
		return collectionlist.NewList[PrefetchOutcomeRow]()
	}
	rows := collectionlist.NewListWithCapacity[PrefetchOutcomeRow](records.Len())
	records.Range(func(_ int, record meta.PrefetchOutcomeRecord) bool {
		rows.Add(PrefetchOutcomeRow{
			Candidate:      record.CandidateKey,
			Status:         record.Status,
			Attempt:        record.Attempt,
			Reason:         record.Reason,
			SkipReason:     record.SkipReason,
			Error:          record.Error,
			NextRetryAt:    formatTime(record.NextRetryAt),
			FinishedAt:     formatTime(record.FinishedAt),
			BytesWarmed:    formatBytes(record.BytesWarmed),
			ManifestDigest: record.ManifestDigest,
		})
		return true
	})
	return rows
}

package prefetch

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Service) startRunRecord(ctx context.Context, opts RunOptions) (*meta.PrefetchRunRecord, error) {
	record, err := s.metadata.CreatePrefetchRun(ctx, meta.PrefetchRunRecord{
		Status:          runStatusRunning,
		Trigger:         "scheduler",
		StartedAt:       opts.Now,
		ByteBudget:      opts.MaxBytes,
		TaskBudget:      opts.MaxTasks,
		RepositoryLimit: opts.MaxRepositories,
	})
	if err != nil {
		return nil, cacheWrap(err, "create prefetch run")
	}
	return record, nil
}

func (s *Service) finishRunRecord(ctx context.Context, record *meta.PrefetchRunRecord, report *RunReport, runErr error) error {
	if s == nil || s.metadata == nil || record == nil {
		return nil
	}
	record.FinishedAt = time.Now().UTC()
	record.Status = runStatusCompleted
	if runErr != nil {
		record.Status = runStatusFailed
		record.Error = runErr.Error()
		if isContextError(runErr) {
			record.Status = runStatusCanceled
		}
	}
	if report != nil {
		if report.Canceled {
			record.Status = runStatusCanceled
		}
		record.ScannedRecords = report.ScannedRecords
		record.SkippedRecords = report.SkippedRecords
		record.Repositories = report.Repositories
		record.SkippedRepositories = report.SkippedRepositories
		record.Candidates = report.Candidates
		record.Prefetched = report.Prefetched
		record.Failed = report.Failed
		record.SkippedCandidates = report.SkippedCandidates
		record.BytesWarmed = report.BytesWarmed
		record.RetryRequested = report.RetryRequested
	}
	if _, err := s.metadata.UpdatePrefetchRun(ctx, *record); err != nil {
		return cacheWrap(err, "update prefetch run")
	}
	return nil
}

func (s *Service) consumeRunControl(ctx context.Context, action string, at time.Time) (bool, error) {
	_, ok, err := s.metadata.ConsumePrefetchControl(ctx, action, at)
	if err != nil {
		return false, cacheWrap(err, "consume prefetch control")
	}
	return ok, nil
}

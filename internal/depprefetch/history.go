package depprefetch

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

func (s *Service) startRun(ctx context.Context, at time.Time) (*meta.PrefetchRunRecord, error) {
	record, err := s.metadata.CreatePrefetchRun(ctx, meta.PrefetchRunRecord{
		Status:    "running",
		Trigger:   s.ecosystem,
		StartedAt: at,
	})
	if err != nil {
		return nil, oops.In("dependency-prefetch").Wrapf(err, "create dependency prefetch run")
	}
	return record, nil
}

func (s *Service) finishRun(
	ctx context.Context,
	record *meta.PrefetchRunRecord,
	report *ecosystem.PrefetchReport,
	runErr error,
	startedAt time.Time,
) error {
	if record == nil {
		return nil
	}
	record.Status = runStatusCompleted
	if runErr != nil {
		record.Status = runStatusFailed
		record.Error = runErr.Error()
	}
	if report != nil {
		record.ScannedRecords = report.ScannedRecords
		record.SkippedRecords = report.SkippedRecords
		record.Repositories = report.Repositories
		record.SkippedRepositories = report.SkippedRepositories
		record.Candidates = report.Candidates
		record.Prefetched = report.Prefetched
		record.Failed = report.Failed
		record.SkippedCandidates = report.SkippedCandidates
		record.BytesWarmed = report.BytesWarmed
	}
	record.FinishedAt = time.Now().UTC()
	if record.StartedAt.IsZero() {
		record.StartedAt = startedAt
	}
	if _, err := s.metadata.UpdatePrefetchRun(ctx, *record); err != nil {
		return oops.In("dependency-prefetch").Wrapf(err, "finish dependency prefetch run")
	}
	return nil
}

func (s *Service) recordOutcome(
	ctx context.Context,
	runID int64,
	candidate Candidate,
	status string,
	attempt int,
	result FetchResult,
	candidateErr error,
	skipReason string,
	nextRetryAt time.Time,
	startedAt time.Time,
	finishedAt time.Time,
) error {
	record := meta.PrefetchOutcomeRecord{
		RunID:           runID,
		Alias:           candidate.ScopedAlias,
		Repository:      candidate.Repository,
		Reference:       candidate.Reference,
		SourceReference: candidate.Reference,
		Status:          status,
		Reason:          "recent pull rewarm",
		Score:           candidate.Score,
		BytesWarmed:     result.BytesWarmed,
		Attempt:         attempt,
		Error:           errorString(candidateErr),
		SkipReason:      skipReason,
		NextRetryAt:     nextRetryAt,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
	}
	if _, err := s.metadata.CreatePrefetchOutcome(ctx, record); err != nil {
		return oops.In("dependency-prefetch").Wrapf(err, "record dependency prefetch outcome")
	}
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

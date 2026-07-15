package depprefetch

import (
	"context"
	"sync/atomic"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/panjf2000/ants/v2"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

type runState struct {
	prefetched        atomic.Int64
	failed            atomic.Int64
	skippedCandidates atomic.Int64
	bytesWarmed       atomic.Int64
}

type candidateExecution struct {
	runID     int64
	candidate Candidate
	attempt   int
	opts      ecosystem.PrefetchOptions
	state     *runState
}

type candidateOutcome struct {
	status      string
	result      FetchResult
	err         error
	skipReason  string
	nextRetryAt time.Time
	startedAt   time.Time
	finishedAt  time.Time
}

func (s *runState) addPrefetched(bytes int64) {
	s.prefetched.Add(1)
	s.bytesWarmed.Add(bytes)
}

func (s *runState) addFailed() {
	s.failed.Add(1)
}

func (s *runState) addSkipped() {
	s.skippedCandidates.Add(1)
}

func (s *runState) apply(report *ecosystem.PrefetchReport) {
	report.Prefetched = int(s.prefetched.Load())
	report.Failed = int(s.failed.Load())
	report.SkippedCandidates = int(s.skippedCandidates.Load())
	report.BytesWarmed = s.bytesWarmed.Load()
}

func (s *Service) prefetchCandidates(
	ctx context.Context,
	opts ecosystem.PrefetchOptions,
	runID int64,
	candidates *collectionlist.List[Candidate],
	report *ecosystem.PrefetchReport,
) error {
	if candidates == nil || candidates.Len() == 0 {
		return nil
	}
	report.Repositories = s.repositories(candidates)
	state := &runState{}
	tasks := collectionlist.MapList(candidates, func(_ int, candidate Candidate) func(context.Context) error {
		return s.prefetchTask(candidateExecution{
			runID:     runID,
			candidate: candidate,
			opts:      opts,
			state:     state,
		})
	})
	err := worker.RunAllSettled(ctx, s.prefetchPool(), tasks)
	state.apply(report)
	if err != nil && isContextError(err) {
		return oops.In("dependency-prefetch").Wrapf(err, "run dependency prefetch tasks")
	}
	if err != nil {
		s.logger.DebugContext(ctx, "dependency prefetch completed with failures", "error", err)
		return oops.Wrapf(err, "dependency prefetch completed with failures")
	}
	return nil
}

func (s *Service) prefetchTask(execution candidateExecution) func(context.Context) error {
	return func(ctx context.Context) error {
		return s.prefetchTaskResult(ctx, execution)
	}
}

func (s *Service) prefetchTaskResult(
	ctx context.Context,
	execution candidateExecution,
) error {
	attempt, skip, err := s.plan(ctx, execution.opts, execution.candidate)
	if err != nil {
		execution.state.addFailed()
		return err
	}
	execution.attempt = attempt
	if skip != "" {
		return s.recordSkippedCandidate(ctx, execution, skip)
	}
	return s.fetchAndRecordCandidate(ctx, execution)
}

func (s *Service) recordSkippedCandidate(
	ctx context.Context,
	execution candidateExecution,
	skip string,
) error {
	startedAt := time.Now().UTC()
	recordErr := s.recordOutcome(ctx, execution, candidateOutcome{
		status:     statusSkipped,
		skipReason: skip,
		startedAt:  startedAt,
		finishedAt: time.Now().UTC(),
	})
	if recordErr != nil {
		return oops.Wrapf(recordErr, "record skipped dependency prefetch outcome")
	}
	execution.state.addSkipped()
	return nil
}

func (s *Service) fetchAndRecordCandidate(
	ctx context.Context,
	execution candidateExecution,
) error {
	startedAt := time.Now().UTC()
	result, err := s.fetch(ctx, execution.candidate)
	outcome := candidateOutcome{
		result:     result,
		err:        err,
		startedAt:  startedAt,
		finishedAt: time.Now().UTC(),
	}
	if err != nil {
		return s.recordFailedCandidate(ctx, execution, outcome)
	}
	return s.recordSuccessCandidate(ctx, execution, outcome)
}

func (s *Service) recordFailedCandidate(
	ctx context.Context,
	execution candidateExecution,
	outcome candidateOutcome,
) error {
	execution.state.addFailed()
	outcome.status = statusFailed
	outcome.nextRetryAt = nextRetryAt(execution.opts, execution.attempt)
	recordErr := s.recordOutcome(ctx, execution, outcome)
	return oops.Wrapf(multierr.Combine(outcome.err, recordErr), "prefetch dependency candidate")
}

func (s *Service) recordSuccessCandidate(
	ctx context.Context,
	execution candidateExecution,
	outcome candidateOutcome,
) error {
	outcome.status = statusSuccess
	if err := s.recordOutcome(ctx, execution, outcome); err != nil {
		return err
	}
	execution.state.addPrefetched(outcome.result.BytesWarmed)
	return nil
}

func (s *Service) plan(ctx context.Context, opts ecosystem.PrefetchOptions, candidate Candidate) (int, string, error) {
	latest, ok, err := s.metadata.LatestPrefetchOutcome(ctx, meta.PrefetchCandidateKey{
		Alias:      candidate.ScopedAlias,
		Repository: candidate.Repository,
		Reference:  candidate.Reference,
	})
	if err != nil {
		return 0, "", oops.In("dependency-prefetch").Wrapf(err, "get latest dependency prefetch outcome")
	}
	attempt := 1
	if ok && latest != nil && latest.Attempt > 0 {
		attempt = latest.Attempt + 1
	}
	if skip := backoffSkip(opts, latest, ok); skip != "" {
		return attempt, skip, nil
	}
	return attempt, "", nil
}

func backoffSkip(opts ecosystem.PrefetchOptions, latest *meta.PrefetchOutcomeRecord, ok bool) string {
	if !ok || latest == nil {
		return ""
	}
	switch latest.Status {
	case statusFailed, statusSkipped:
		if !latest.NextRetryAt.After(opts.Now) {
			return ""
		}
		return "failure backoff until " + latest.NextRetryAt.Format(time.RFC3339)
	case statusSuccess:
		if opts.RetryWindow <= 0 || latest.FinishedAt.IsZero() || !latest.FinishedAt.Add(opts.RetryWindow).After(opts.Now) {
			return ""
		}
		return "recent success at " + latest.FinishedAt.Format(time.RFC3339)
	default:
		return ""
	}
}

func nextRetryAt(opts ecosystem.PrefetchOptions, attempt int) time.Time {
	if opts.FailureBackoff <= 0 {
		return time.Time{}
	}
	if attempt <= 0 {
		attempt = 1
	}
	delay := opts.FailureBackoff * time.Duration(attempt)
	if opts.RetryWindow > 0 && delay > opts.RetryWindow {
		delay = opts.RetryWindow
	}
	return opts.Now.Add(delay)
}

func (s *Service) prefetchPool() *ants.Pool {
	if s == nil || s.workers == nil {
		return nil
	}
	return s.workers.IOPool()
}

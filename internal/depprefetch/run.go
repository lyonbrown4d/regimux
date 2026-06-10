package depprefetch

import (
	"context"
	"sync"
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
	mu                sync.Mutex
	prefetched        int
	failed            int
	skippedCandidates int
	bytesWarmed       int64
}

func (s *runState) addPrefetched(bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefetched++
	s.bytesWarmed += bytes
}

func (s *runState) addFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed++
}

func (s *runState) addSkipped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skippedCandidates++
}

func (s *runState) apply(report *ecosystem.PrefetchReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	report.Prefetched = s.prefetched
	report.Failed = s.failed
	report.SkippedCandidates = s.skippedCandidates
	report.BytesWarmed = s.bytesWarmed
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
		return s.prefetchTask(opts, runID, candidate, state)
	})
	err := worker.RunAllSettled(ctx, s.prefetchPool(), tasks)
	state.apply(report)
	if err != nil && isContextError(err) {
		return oops.In("dependency-prefetch").Wrapf(err, "run dependency prefetch tasks")
	}
	if err != nil {
		s.logger.DebugContext(ctx, "dependency prefetch completed with failures", "error", err)
	}
	return nil
}

func (s *Service) prefetchTask(
	opts ecosystem.PrefetchOptions,
	runID int64,
	candidate Candidate,
	state *runState,
) func(context.Context) error {
	return func(ctx context.Context) error {
		startedAt := time.Now().UTC()
		attempt, skip, err := s.plan(ctx, opts, candidate)
		if err != nil {
			state.addFailed()
			return err
		}
		if skip != "" {
			state.addSkipped()
			return s.recordOutcome(ctx, runID, candidate, statusSkipped, attempt, FetchResult{}, nil, skip, time.Time{}, startedAt, time.Now().UTC())
		}
		result, err := s.fetch(ctx, candidate)
		finishedAt := time.Now().UTC()
		if err != nil {
			state.addFailed()
			nextRetryAt := nextRetryAt(opts, attempt)
			recordErr := s.recordOutcome(ctx, runID, candidate, statusFailed, attempt, result, err, "", nextRetryAt, startedAt, finishedAt)
			return oops.Wrapf(multierr.Combine(err, recordErr), "prefetch dependency candidate")
		}
		state.addPrefetched(result.BytesWarmed)
		return s.recordOutcome(ctx, runID, candidate, statusSuccess, attempt, result, nil, "", time.Time{}, startedAt, finishedAt)
	}
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
	if !ok || latest == nil || !latest.NextRetryAt.After(opts.Now) {
		return ""
	}
	switch latest.Status {
	case statusFailed, statusSkipped:
		return "failure backoff until " + latest.NextRetryAt.Format(time.RFC3339)
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
	return s.workers.PrefetchPool()
}

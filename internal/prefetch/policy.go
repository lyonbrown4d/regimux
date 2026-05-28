package prefetch

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const (
	runStatusRunning   = "running"
	runStatusCompleted = "completed"
	runStatusFailed    = "failed"
	runStatusCanceled  = "canceled"

	outcomeStatusSuccess = "success"
	outcomeStatusFailed  = "failed"
	outcomeStatusSkipped = "skipped"

	prefetchControlCancel = "cancel"
	prefetchControlRetry  = "retry"
)

var errPrefetchBudgetExceeded = errors.New("prefetch budget exceeded")

type ControlReport struct {
	Action    string
	ActiveRun bool
	At        time.Time
}

type candidatePlan struct {
	candidate   Candidate
	attempt     int
	nextRetryAt time.Time
}

type runExecution struct {
	metadata       meta.Store
	runID          int64
	opts           RunOptions
	retryRequested bool

	mu            sync.Mutex
	tasksReserved int
	bytesReserved int64
	bytesWarmed   int64
}

func newRunExecution(metadata meta.Store, runID int64, opts RunOptions, retryRequested bool) *runExecution {
	return &runExecution{
		metadata:       metadata,
		runID:          runID,
		opts:           opts,
		retryRequested: retryRequested,
	}
}

func (e *runExecution) reserveTask() bool {
	if e == nil || e.opts.MaxTasks <= 0 {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tasksReserved >= e.opts.MaxTasks {
		return false
	}
	e.tasksReserved++
	return true
}

func (e *runExecution) reserveBytes(bytes int64) bool {
	if e == nil || e.opts.MaxBytes <= 0 || bytes <= 0 {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.bytesReserved+bytes > e.opts.MaxBytes {
		return false
	}
	e.bytesReserved += bytes
	return true
}

func (e *runExecution) addBytesWarmed(bytes int64) {
	if e == nil || bytes <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bytesWarmed += bytes
}

func (e *runExecution) bytesWarmedSnapshot() int64 {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.bytesWarmed
}

func (e *runExecution) planCandidate(ctx context.Context, candidate Candidate) (candidatePlan, string, error) {
	if e == nil {
		return candidatePlan{candidate: candidate, attempt: 1}, "", nil
	}
	latest, ok, err := e.latestOutcome(ctx, candidate)
	if err != nil {
		return candidatePlan{}, "", err
	}
	attempt := nextCandidateAttempt(latest, ok)
	if nextRetryAt, reason, skip := e.backoffSkip(latest, ok); skip {
		return candidatePlan{candidate: candidate, attempt: attempt, nextRetryAt: nextRetryAt}, reason, nil
	}
	if !e.reserveTask() {
		return candidatePlan{candidate: candidate, attempt: attempt}, "task budget reached", nil
	}
	return candidatePlan{candidate: candidate, attempt: attempt}, "", nil
}

func nextCandidateAttempt(latest *meta.PrefetchOutcomeRecord, ok bool) int {
	if ok && latest.Attempt > 0 {
		return latest.Attempt + 1
	}
	return 1
}

func (e *runExecution) backoffSkip(latest *meta.PrefetchOutcomeRecord, ok bool) (time.Time, string, bool) {
	if !ok || e.retryRequested || latest == nil || !latest.NextRetryAt.After(e.opts.Now) {
		return time.Time{}, "", false
	}
	switch latest.Status {
	case outcomeStatusFailed, outcomeStatusSkipped:
		return latest.NextRetryAt, "failure backoff until " + latest.NextRetryAt.Format(time.RFC3339), true
	default:
		return time.Time{}, "", false
	}
}

func (e *runExecution) latestOutcome(ctx context.Context, candidate Candidate) (*meta.PrefetchOutcomeRecord, bool, error) {
	if e == nil || e.metadata == nil {
		return nil, false, nil
	}
	record, ok, err := e.metadata.LatestPrefetchOutcome(ctx, meta.PrefetchCandidateKey{
		Alias:      candidate.Alias,
		Repository: candidate.Repo,
		Reference:  candidate.Tag,
	})
	if err != nil {
		return nil, false, cacheWrap(err, "get latest prefetch outcome")
	}
	return record, ok, nil
}

func (e *runExecution) recordSkipped(ctx context.Context, plan candidatePlan, skipReason string) error {
	return e.recordOutcome(ctx, candidateOutcome{
		candidate:   plan.candidate,
		status:      outcomeStatusSkipped,
		attempt:     plan.attempt,
		skipReason:  skipReason,
		nextRetryAt: plan.nextRetryAt,
		startedAt:   e.opts.Now,
		finishedAt:  e.opts.Now,
	})
}

func (e *runExecution) recordOutcome(ctx context.Context, outcome candidateOutcome) error {
	if e == nil || e.metadata == nil || e.runID == 0 {
		return nil
	}
	if ctx == nil {
		return cacheError("prefetch outcome context is required")
	}
	record := meta.PrefetchOutcomeRecord{
		RunID:              e.runID,
		Alias:              outcome.candidate.Alias,
		Repository:         outcome.candidate.Repo,
		Reference:          outcome.candidate.Tag,
		SourceReference:    outcome.candidate.SourceTag,
		Status:             outcome.status,
		Reason:             outcome.candidate.Reason,
		Score:              outcome.candidate.Score,
		ManifestDigest:     outcome.result.manifestDigest,
		LayerCount:         outcome.result.layerCount,
		BlobCount:          outcome.result.blobCount,
		ChildManifestCount: outcome.result.childManifestCount,
		BytesWarmed:        outcome.result.bytesWarmed,
		Attempt:            outcome.attempt,
		Error:              errorString(outcome.err),
		SkipReason:         outcome.skipReason,
		NextRetryAt:        outcome.nextRetryAt,
		StartedAt:          outcome.startedAt,
		FinishedAt:         outcome.finishedAt,
	}
	if _, err := e.metadata.CreatePrefetchOutcome(ctx, record); err != nil {
		return cacheWrap(err, "record prefetch outcome")
	}
	return nil
}

func (e *runExecution) nextRetryAt(attempt int) time.Time {
	if e == nil || e.opts.FailureBackoff <= 0 {
		return time.Time{}
	}
	if attempt <= 0 {
		attempt = 1
	}
	delay := e.opts.FailureBackoff * time.Duration(attempt)
	if e.opts.RetryWindow > 0 && delay > e.opts.RetryWindow {
		delay = e.opts.RetryWindow
	}
	return e.opts.Now.Add(delay)
}

type candidateOutcome struct {
	candidate   Candidate
	status      string
	result      prefetchResult
	attempt     int
	err         error
	skipReason  string
	nextRetryAt time.Time
	startedAt   time.Time
	finishedAt  time.Time
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

package prefetch

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/panjf2000/ants/v2"
)

func (s *Service) prefetchRepository(
	ctx context.Context,
	route repoKey,
	records *collectionlist.List[meta.PullRecord],
	opts RunOptions,
	execution *runExecution,
) (repositoryPrefetchReport, error) {
	tags, err := s.availableTags(ctx, route, opts.TagsPageSize)
	if err != nil {
		s.logger.WarnContext(ctx, "prefetch tags discovery failed", "alias", route.alias, "repository", route.repo, "error", err)
		return repositoryPrefetchReport{prefetched: collectionlist.NewList[string](), failed: 1}, nil
	}

	candidates := GenerateCandidates(toCandidateRecords(records), tags, Options{
		MaxCandidates:      opts.MaxCandidatesPerRepo,
		MaxVersionDistance: opts.MaxVersionDistance,
		Now:                opts.Now,
	})
	state := newPrefetchTaskState(candidates.Len())
	tasks := s.buildPrefetchTasks(ctx, opts, execution, candidates, state)
	if err := worker.RunAll(ctx, s.prefetchPool(), tasks); err != nil {
		if isContextError(err) {
			return repositoryPrefetchReport{}, cacheWrap(err, "prefetch repository")
		}
		s.logger.DebugContext(ctx, "prefetch repository completed with failures", "error", err)
	}
	return state.report(candidates.Len()), nil
}

func (s *Service) buildPrefetchTasks(
	ctx context.Context,
	opts RunOptions,
	execution *runExecution,
	candidates *collectionlist.List[Candidate],
	state *prefetchTaskState,
) *collectionlist.List[func(context.Context) error] {
	tasks := collectionlist.NewListWithCapacity[func(context.Context) error](candidates.Len())
	candidates.Range(func(_ int, candidate Candidate) bool {
		plan, skipReason, err := execution.planCandidate(ctx, candidate)
		switch {
		case err != nil:
			state.failed.Add(1)
			s.logPrefetchFailure(ctx, candidate, prefetchResult{}, err)
		case skipReason != "":
			s.recordSkippedCandidate(ctx, execution, state, plan, skipReason)
		default:
			tasks.Add(s.prefetchTask(opts, execution, plan, state))
		}
		return true
	})
	return tasks
}

func (s *Service) recordSkippedCandidate(
	ctx context.Context,
	execution *runExecution,
	state *prefetchTaskState,
	plan candidatePlan,
	skipReason string,
) {
	state.skipped.Add(1)
	if err := execution.recordSkipped(ctx, plan, skipReason); err != nil {
		state.failed.Add(1)
		s.logPrefetchFailure(ctx, plan.candidate, prefetchResult{}, err)
	}
}

func (s *Service) prefetchTask(
	opts RunOptions,
	execution *runExecution,
	plan candidatePlan,
	state *prefetchTaskState,
) func(context.Context) error {
	return func(taskCtx context.Context) error {
		startedAt := time.Now().UTC()
		candidate := plan.candidate
		result, err := s.prefetchCandidate(taskCtx, opts, execution, candidate)
		finishedAt := time.Now().UTC()
		if err != nil {
			return s.finishFailedPrefetchTask(taskCtx, execution, plan, state, result, err, startedAt, finishedAt)
		}
		return s.finishSuccessfulPrefetchTask(taskCtx, execution, plan, state, result, startedAt, finishedAt)
	}
}

func (s *Service) finishFailedPrefetchTask(
	ctx context.Context,
	execution *runExecution,
	plan candidatePlan,
	state *prefetchTaskState,
	result prefetchResult,
	err error,
	startedAt time.Time,
	finishedAt time.Time,
) error {
	status, skipReason, nextRetryAt := execution.failureOutcome(err, plan.attempt)
	if status == outcomeStatusSkipped {
		state.skipped.Add(1)
	} else {
		state.failed.Add(1)
	}
	recordErr := execution.recordOutcome(ctx, candidateOutcome{
		candidate:   plan.candidate,
		status:      status,
		result:      result,
		attempt:     plan.attempt,
		err:         err,
		skipReason:  skipReason,
		nextRetryAt: nextRetryAt,
		startedAt:   startedAt,
		finishedAt:  finishedAt,
	})
	if recordErr != nil {
		state.failed.Add(1)
		s.logPrefetchFailure(ctx, plan.candidate, result, recordErr)
	}
	s.logPrefetchFailure(ctx, plan.candidate, result, err)
	if isContextError(err) {
		return err
	}
	return nil
}

func (s *Service) finishSuccessfulPrefetchTask(
	ctx context.Context,
	execution *runExecution,
	plan candidatePlan,
	state *prefetchTaskState,
	result prefetchResult,
	startedAt time.Time,
	finishedAt time.Time,
) error {
	execution.addBytesWarmed(result.bytesWarmed)
	if err := execution.recordOutcome(ctx, candidateOutcome{
		candidate:  plan.candidate,
		status:     outcomeStatusSuccess,
		result:     result,
		attempt:    plan.attempt,
		startedAt:  startedAt,
		finishedAt: finishedAt,
	}); err != nil {
		state.failed.Add(1)
		s.logPrefetchFailure(ctx, plan.candidate, result, err)
		return nil
	}
	state.addPrefetched(plan.candidate)
	s.logPrefetchSuccess(ctx, plan.candidate, result)
	return nil
}

func (e *runExecution) failureOutcome(err error, attempt int) (string, string, time.Time) {
	switch {
	case errors.Is(err, errPrefetchBudgetExceeded):
		return outcomeStatusSkipped, "byte budget reached", time.Time{}
	case isContextError(err):
		return outcomeStatusSkipped, "run canceled", time.Time{}
	default:
		return outcomeStatusFailed, "", e.nextRetryAt(attempt)
	}
}

func (s *Service) prefetchCandidate(ctx context.Context, opts RunOptions, execution *runExecution, candidate Candidate) (prefetchResult, error) {
	manifest, err := s.refreshManifest(ctx, cache.ManifestRequest{
		UpstreamAlias:  candidate.Alias,
		Repo:           candidate.Repo,
		Reference:      candidate.Tag,
		Accept:         opts.Accept,
		SkipPullRecord: true,
	})
	if err != nil {
		return prefetchResult{}, cacheWrap(err, "prefetch manifest")
	}
	return s.prefetchManifestArtifacts(ctx, opts, execution, candidate, candidate.Tag, manifest, 0)
}

func (s *Service) logPrefetchFailure(ctx context.Context, candidate Candidate, result prefetchResult, err error) {
	s.logger.WarnContext(ctx, "prefetch candidate failed",
		"alias", candidate.Alias,
		"repository", candidate.Repo,
		"reference", candidate.Tag,
		"digest", result.manifestDigest,
		"layer_count", result.layerCount,
		"blob_count", result.blobCount,
		"child_manifest_count", result.childManifestCount,
		"reason", candidate.Reason,
		"score", candidate.Score,
		"error", err,
	)
}

func (s *Service) logPrefetchSuccess(ctx context.Context, candidate Candidate, result prefetchResult) {
	s.logger.InfoContext(ctx, "prefetched manifest artifacts",
		"alias", candidate.Alias,
		"repository", candidate.Repo,
		"reference", candidate.Tag,
		"digest", result.manifestDigest,
		"layer_count", result.layerCount,
		"blob_count", result.blobCount,
		"child_manifest_count", result.childManifestCount,
		"reason", candidate.Reason,
		"score", candidate.Score,
	)
}

func (s *Service) prefetchPool() *ants.Pool {
	if s == nil || s.workers == nil {
		return nil
	}
	return s.workers.IOPool()
}

func (s *Service) availableTags(ctx context.Context, route repoKey, pageSize int) (*collectionlist.List[string], error) {
	result, err := s.tags.List(ctx, cache.TagRequest{
		UpstreamAlias: route.alias,
		Repo:          route.repo,
		N:             strconv.Itoa(pageSize),
	})
	if err != nil {
		return nil, cacheWrap(err, "list tags for prefetch")
	}
	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(result.Body, &body); err != nil {
		return nil, cacheWrap(err, "decode tags response for prefetch")
	}
	return collectionlist.NewList(body.Tags...), nil
}

type prefetchTaskState struct {
	prefetched *collectionlist.List[string]
	failed     atomic.Int32
	skipped    atomic.Int32
	mu         sync.Mutex
}

func newPrefetchTaskState(candidateCount int) *prefetchTaskState {
	return &prefetchTaskState{
		prefetched: collectionlist.NewListWithCapacity[string](candidateCount),
	}
}

func (s *prefetchTaskState) addPrefetched(candidate Candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefetched.Add(candidate.Alias + "/" + candidate.Repo + ":" + candidate.Tag)
}

func (s *prefetchTaskState) report(candidateCount int) repositoryPrefetchReport {
	return repositoryPrefetchReport{
		prefetched: s.prefetched,
		candidates: candidateCount,
		failed:     int(s.failed.Load()),
		skipped:    int(s.skipped.Load()),
	}
}

type repoKey struct {
	alias string
	repo  string
}

type repositoryPrefetchReport struct {
	prefetched *collectionlist.List[string]
	candidates int
	failed     int
	skipped    int
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

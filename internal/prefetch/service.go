package prefetch

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/panjf2000/ants/v2"
)

const (
	defaultMaxRecords   = 200
	defaultTagsPageSize = 1000
	defaultMinPullCount = 1
	prefetchLogGroup    = "prefetch"
)

type Service struct {
	metadata  meta.Store
	tags      cache.TagService
	manifests cache.ManifestService
	workers   *worker.Pools
	logger    *slog.Logger
}

type RunOptions struct {
	MaxRecords           int
	MinPullCount         int64
	TagsPageSize         int
	MaxCandidatesPerRepo int
	MaxVersionDistance   int
	Accept               string
	Now                  time.Time
}

type RunReport struct {
	ScannedRecords   int
	SkippedRecords   int
	Repositories     int
	Candidates       int
	Prefetched       int
	Failed           int
	PrefetchedRoutes []string
}

type ServiceDependencies struct {
	Metadata  meta.Store
	Tags      cache.TagService
	Manifests cache.ManifestService
	Logger    *slog.Logger
	Workers   *worker.Pools
}

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		metadata:  deps.Metadata,
		tags:      deps.Tags,
		manifests: deps.Manifests,
		workers:   deps.Workers,
		logger:    logger.With("component", prefetchLogGroup),
	}
}

func (s *Service) Run(ctx context.Context, opts RunOptions) (*RunReport, error) {
	if ctx == nil {
		return nil, cacheError("prefetch context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, cacheWrap(err, "prefetch context")
	}
	if s == nil || s.metadata == nil || s.tags == nil || s.manifests == nil {
		return nil, cacheError("prefetch service is not configured")
	}
	opts = normalizeRunOptions(opts)

	records, err := s.metadata.ListPulls(ctx)
	if err != nil {
		return nil, cacheWrap(err, "list pull records for prefetch")
	}
	scannedRecords := len(records)
	records = filterPullRecords(records, opts)

	report := &RunReport{
		ScannedRecords: scannedRecords,
		SkippedRecords: scannedRecords - len(records),
	}
	groups := groupPullRecords(records)
	report.Repositories = len(groups)
	for route, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, cacheWrap(err, "prefetch context")
		}
		prefetchedRoutes, candidates, failed := s.prefetchRepository(ctx, route, group, opts)
		report.Prefetched += len(prefetchedRoutes)
		report.Candidates += candidates
		report.Failed += failed
		report.PrefetchedRoutes = append(report.PrefetchedRoutes, prefetchedRoutes...)
	}
	return report, nil
}

func (s *Service) prefetchRepository(ctx context.Context, route repoKey, records []meta.PullRecord, opts RunOptions) ([]string, int, int) {
	tags, err := s.availableTags(ctx, route, opts.TagsPageSize)
	if err != nil {
		s.logger.WarnContext(ctx, "prefetch tags discovery failed", "alias", route.alias, "repository", route.repo, "error", err)
		return nil, 0, 1
	}
	candidates := GenerateCandidates(toCandidateRecords(records), tags, Options{
		MaxCandidates:      opts.MaxCandidatesPerRepo,
		MaxVersionDistance: opts.MaxVersionDistance,
		Now:                opts.Now,
	})

	prefetched := make([]string, 0, len(candidates))
	var failed atomic.Int32
	var mu sync.Mutex
	tasks := make([]func(context.Context) error, 0, len(candidates))
	for i := range candidates {
		tasks = append(tasks, s.prefetchTask(opts, candidates[i], &failed, &mu, &prefetched))
	}
	if err := worker.RunAll(ctx, s.prefetchPool(), tasks); err != nil {
		s.logger.DebugContext(ctx, "prefetch repository completed with failures", "error", err)
	}
	return prefetched, len(candidates), int(failed.Load())
}

func (s *Service) prefetchTask(
	opts RunOptions,
	candidate Candidate,
	failed *atomic.Int32,
	mu *sync.Mutex,
	prefetched *[]string,
) func(context.Context) error {
	return func(taskCtx context.Context) error {
		if err := s.prefetchCandidate(taskCtx, opts, candidate); err != nil {
			failed.Add(1)
			s.logPrefetchFailure(taskCtx, candidate, err)
			return err
		}
		mu.Lock()
		*prefetched = append(*prefetched, candidate.Alias+"/"+candidate.Repo+":"+candidate.Tag)
		mu.Unlock()
		s.logPrefetchSuccess(taskCtx, candidate)
		return nil
	}
}

func (s *Service) prefetchCandidate(ctx context.Context, opts RunOptions, candidate Candidate) error {
	_, err := s.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias:  candidate.Alias,
		Repo:           candidate.Repo,
		Reference:      candidate.Tag,
		Accept:         opts.Accept,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return cacheWrap(err, "prefetch manifest")
	}
	return nil
}

func (s *Service) logPrefetchFailure(ctx context.Context, candidate Candidate, err error) {
	s.logger.WarnContext(ctx, "prefetch manifest failed",
		"alias", candidate.Alias,
		"repository", candidate.Repo,
		"reference", candidate.Tag,
		"reason", candidate.Reason,
		"score", candidate.Score,
		"error", err,
	)
}

func (s *Service) logPrefetchSuccess(ctx context.Context, candidate Candidate) {
	s.logger.InfoContext(ctx, "prefetched manifest",
		"alias", candidate.Alias,
		"repository", candidate.Repo,
		"reference", candidate.Tag,
		"reason", candidate.Reason,
		"score", candidate.Score,
	)
}

func (s *Service) prefetchPool() *ants.Pool {
	if s == nil || s.workers == nil {
		return nil
	}
	return s.workers.PrefetchPool()
}

func (s *Service) availableTags(ctx context.Context, route repoKey, pageSize int) ([]string, error) {
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
	return body.Tags, nil
}

type repoKey struct {
	alias string
	repo  string
}

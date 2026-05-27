package prefetch

import (
	"context"
	"encoding/json"
	"errors"
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
	blobs     cache.BlobService
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
	Blobs     cache.BlobService
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
		blobs:     deps.Blobs,
		workers:   deps.Workers,
		logger:    logger.With("component", prefetchLogGroup),
	}
}

func (s *Service) Run(ctx context.Context, opts RunOptions) (*RunReport, error) {
	if err := s.validateRun(ctx); err != nil {
		return nil, err
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
	if err := s.prefetchGroups(ctx, groups, opts, report); err != nil {
		return nil, err
	}
	return report, nil
}

func (s *Service) validateRun(ctx context.Context) error {
	if ctx == nil {
		return cacheError("prefetch context is required")
	}
	if err := ctx.Err(); err != nil {
		return cacheWrap(err, "prefetch context")
	}
	if s == nil || s.metadata == nil || s.tags == nil || s.manifests == nil {
		return cacheError("prefetch service is not configured")
	}
	return nil
}

func (s *Service) prefetchGroups(ctx context.Context, groups map[repoKey][]meta.PullRecord, opts RunOptions, report *RunReport) error {
	for route, group := range groups {
		if err := ctx.Err(); err != nil {
			return cacheWrap(err, "prefetch context")
		}
		prefetchedRoutes, candidates, failed, err := s.prefetchRepository(ctx, route, group, opts)
		if err != nil {
			return err
		}
		report.Prefetched += len(prefetchedRoutes)
		report.Candidates += candidates
		report.Failed += failed
		report.PrefetchedRoutes = append(report.PrefetchedRoutes, prefetchedRoutes...)
	}
	return nil
}

func (s *Service) prefetchRepository(ctx context.Context, route repoKey, records []meta.PullRecord, opts RunOptions) ([]string, int, int, error) {
	tags, err := s.availableTags(ctx, route, opts.TagsPageSize)
	if err != nil {
		s.logger.WarnContext(ctx, "prefetch tags discovery failed", "alias", route.alias, "repository", route.repo, "error", err)
		return nil, 0, 1, nil
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
		if isContextError(err) {
			return nil, len(candidates), int(failed.Load()), cacheWrap(err, "prefetch repository")
		}
		s.logger.DebugContext(ctx, "prefetch repository completed with failures", "error", err)
	}
	return prefetched, len(candidates), int(failed.Load()), nil
}

func (s *Service) prefetchTask(
	opts RunOptions,
	candidate Candidate,
	failed *atomic.Int32,
	mu *sync.Mutex,
	prefetched *[]string,
) func(context.Context) error {
	return func(taskCtx context.Context) error {
		result, err := s.prefetchCandidate(taskCtx, opts, candidate)
		if err != nil {
			failed.Add(1)
			s.logPrefetchFailure(taskCtx, candidate, result, err)
			if isContextError(err) {
				return err
			}
			return nil
		}
		mu.Lock()
		*prefetched = append(*prefetched, candidate.Alias+"/"+candidate.Repo+":"+candidate.Tag)
		mu.Unlock()
		s.logPrefetchSuccess(taskCtx, candidate, result)
		return nil
	}
}

func (s *Service) prefetchCandidate(ctx context.Context, opts RunOptions, candidate Candidate) (prefetchResult, error) {
	manifest, err := s.manifests.Get(ctx, cache.ManifestRequest{
		UpstreamAlias:  candidate.Alias,
		Repo:           candidate.Repo,
		Reference:      candidate.Tag,
		Accept:         opts.Accept,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return prefetchResult{}, cacheWrap(err, "prefetch manifest")
	}
	return s.prefetchManifestArtifacts(ctx, opts, candidate, candidate.Tag, manifest, 0)
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

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

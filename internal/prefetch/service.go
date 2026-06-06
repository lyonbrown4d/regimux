package prefetch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"go.uber.org/multierr"
)

const (
	defaultMaxRecords   = 200
	defaultTagsPageSize = 1000
	defaultMinPullCount = 1
	prefetchLogGroup    = "prefetch"
)

type Service struct {
	metadata        meta.Store
	tags            cache.TagService
	manifests       cache.ManifestService
	blobs           cache.BlobService
	workers         *worker.Pools
	logger          *slog.Logger
	syncJobs        *collectionmapping.ConcurrentMap[string, SyncJob]
	activeMu        sync.Mutex
	activeRunID     int64
	activeRunCancel context.CancelFunc
}

type RunOptions struct {
	MaxRecords           int
	MinPullCount         int64
	TagsPageSize         int
	MaxCandidatesPerRepo int
	MaxVersionDistance   int
	Accept               string
	ManifestOnly         bool
	MaxBytes             int64
	MaxTasks             int
	MaxRepositories      int
	FailureBackoff       time.Duration
	RetryWindow          time.Duration
	Now                  time.Time
}

type RunReport struct {
	ScannedRecords      int
	SkippedRecords      int
	Repositories        int
	SkippedRepositories int
	Candidates          int
	Prefetched          int
	Failed              int
	SkippedCandidates   int
	BytesWarmed         int64
	RetryRequested      bool
	Canceled            bool
	PrefetchedRoutes    []string
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
		syncJobs:  collectionmapping.NewConcurrentMap[string, SyncJob](),
	}
}

func (s *Service) Run(ctx context.Context, opts RunOptions) (*RunReport, error) {
	if err := s.validateRun(ctx); err != nil {
		return nil, err
	}
	opts = normalizeRunOptions(opts)
	startedAt := time.Now()
	s.logger.InfoContext(ctx,
		"prefetch run starting",
		"max_records", opts.MaxRecords,
		"min_pull_count", opts.MinPullCount,
		"max_candidates_per_repo", opts.MaxCandidatesPerRepo,
		"max_version_distance", opts.MaxVersionDistance,
		"manifest_only", opts.ManifestOnly,
		"max_bytes", opts.MaxBytes,
		"max_tasks", opts.MaxTasks,
		"max_repositories", opts.MaxRepositories,
	)
	run, err := s.startRunRecord(ctx, opts)
	if err != nil {
		return nil, err
	}
	report := &RunReport{}
	retryRequested, cancelRequested, err := s.consumeRunControls(ctx, opts.Now)
	report.RetryRequested = retryRequested
	if err != nil {
		finishErr := s.finishRunRecord(ctx, run, report, err)
		return nil, cacheWrap(multierr.Combine(err, finishErr), "finish prefetch run after control failure")
	}
	if cancelRequested {
		return s.finishCanceledRun(ctx, run, report)
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.setActiveCancel(run.ID, cancel)
	defer s.clearActiveCancel(run.ID)
	defer cancel()

	execution := newRunExecution(s.metadata, run.ID, opts, retryRequested)
	runReport, err := s.run(runCtx, opts, execution)
	if runReport != nil {
		report = runReport
		report.RetryRequested = retryRequested
		report.BytesWarmed = execution.bytesWarmedSnapshot()
	}
	finishErr := s.finishRunRecord(ctx, run, report, err)
	if err != nil {
		s.logger.WarnContext(ctx, "prefetch run failed", "run_id", run.ID, "duration", time.Since(startedAt), "error", err)
		return report, cacheWrap(multierr.Combine(err, finishErr), "finish prefetch run after failure")
	}
	if finishErr != nil {
		return report, finishErr
	}
	s.logger.InfoContext(ctx,
		"prefetch run completed",
		"run_id", run.ID,
		"duration", time.Since(startedAt),
		"scanned_records", report.ScannedRecords,
		"repositories", report.Repositories,
		"candidates", report.Candidates,
		"prefetched", report.Prefetched,
		"failed", report.Failed,
		"skipped_candidates", report.SkippedCandidates,
		"bytes_warmed", report.BytesWarmed,
		"retry_requested", report.RetryRequested,
		"canceled", report.Canceled,
	)
	return report, nil
}

func (s *Service) consumeRunControls(ctx context.Context, at time.Time) (bool, bool, error) {
	retryRequested, err := s.consumeRunControl(ctx, prefetchControlRetry, at)
	if err != nil {
		return false, false, err
	}
	cancelRequested, err := s.consumeRunControl(ctx, prefetchControlCancel, at)
	if err != nil {
		return retryRequested, false, err
	}
	return retryRequested, cancelRequested, nil
}

func (s *Service) finishCanceledRun(ctx context.Context, run *meta.PrefetchRunRecord, report *RunReport) (*RunReport, error) {
	report.Canceled = true
	if finishErr := s.finishRunRecord(ctx, run, report, nil); finishErr != nil {
		return nil, finishErr
	}
	s.logger.InfoContext(ctx, "prefetch run canceled before execution", "run_id", run.ID)
	return report, nil
}

func (s *Service) run(ctx context.Context, opts RunOptions, execution *runExecution) (*RunReport, error) {
	records, err := s.metadata.ListPulls(ctx)
	if err != nil {
		return nil, cacheWrap(err, "list pull records for prefetch")
	}
	scannedRecords := len(records)
	filteredRecords := filterPullRecords(collectionlist.NewList(records...), opts)

	report := &RunReport{
		ScannedRecords: scannedRecords,
		SkippedRecords: scannedRecords - filteredRecords.Len(),
	}
	groups := groupPullRecords(filteredRecords)
	report.Repositories = groups.Len()
	if err := s.prefetchGroups(ctx, groups, opts, execution, report); err != nil {
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

func (s *Service) prefetchGroups(
	ctx context.Context,
	groups *collectionmapping.MultiMap[repoKey, meta.PullRecord],
	opts RunOptions,
	execution *runExecution,
	report *RunReport,
) error {
	var runErr error
	processedRepositories := 0
	groups.RangeView(func(route repoKey, group []meta.PullRecord) bool {
		if err := ctx.Err(); err != nil {
			runErr = cacheWrap(err, "prefetch context")
			return false
		}
		if opts.MaxRepositories > 0 && processedRepositories >= opts.MaxRepositories {
			report.SkippedRepositories++
			return true
		}
		processedRepositories++
		repositoryReport, err := s.prefetchRepository(ctx, route, collectionlist.NewList(group...), opts, execution)
		if err != nil {
			runErr = err
			return false
		}
		report.Prefetched += repositoryReport.prefetched.Len()
		report.Candidates += repositoryReport.candidates
		report.Failed += repositoryReport.failed
		report.SkippedCandidates += repositoryReport.skipped
		report.PrefetchedRoutes = append(report.PrefetchedRoutes, repositoryReport.prefetched.Values()...)
		return true
	})
	if runErr != nil {
		return runErr
	}
	return nil
}

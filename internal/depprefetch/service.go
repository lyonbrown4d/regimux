package depprefetch

import (
	"context"
	"log/slog"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/lo"
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

func New(deps Dependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		ecosystem: strings.TrimSpace(deps.Ecosystem),
		metadata:  deps.Metadata,
		workers:   deps.Workers,
		logger:    logger.With("component", "dependency-prefetch", "ecosystem", deps.Ecosystem),
		fetch:     deps.Fetch,
	}
}

func Capability(name string, upstreams *collectionlist.List[ecosystem.Upstream]) ecosystem.Capability {
	targets := ecosystem.CapabilityTargets(upstreams)
	if targets.Len() == 0 {
		return ecosystem.DisabledCapability(name+" prefetch has no configured upstream", upstreams)
	}
	return ecosystem.EnabledCapability(name+" recent pull prefetch is enabled", targets)
}

func (s *Service) Prefetch(ctx context.Context, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if err := s.validate(ctx); err != nil {
		return nil, err
	}
	opts = normalizeOptions(opts)
	startedAt := time.Now().UTC()
	run, err := s.startRun(ctx, startedAt)
	if err != nil {
		return nil, err
	}

	report, runErr := s.run(ctx, opts, run.ID)
	finishErr := s.finishRun(ctx, run, report, runErr, startedAt)
	if runErr != nil {
		return report, oops.Wrapf(multierr.Combine(runErr, finishErr), "run dependency prefetch")
	}
	if finishErr != nil {
		return report, finishErr
	}
	return report, nil
}

func (s *Service) run(ctx context.Context, opts ecosystem.PrefetchOptions, runID int64) (*ecosystem.PrefetchReport, error) {
	records, err := s.metadata.ListPulls(ctx, meta.PullListRecentFirst())
	if err != nil {
		return nil, oops.In("dependency-prefetch").Wrapf(err, "list dependency pull records")
	}
	candidates := s.candidates(collectionlist.NewList(records...), opts)
	report := &ecosystem.PrefetchReport{
		Ecosystem:      s.ecosystem,
		ScannedRecords: len(records),
		SkippedRecords: len(records) - candidates.Len(),
		Candidates:     candidates.Len(),
	}
	return report, s.prefetchCandidates(ctx, opts, runID, candidates, report)
}

func (s *Service) validate(ctx context.Context) error {
	if ctx == nil {
		return oops.In("dependency-prefetch").Errorf("prefetch context is required")
	}
	if err := ctx.Err(); err != nil {
		return oops.In("dependency-prefetch").Wrapf(err, "prefetch context")
	}
	if s == nil || s.metadata == nil || s.fetch == nil || s.ecosystem == "" {
		return oops.In("dependency-prefetch").Errorf("dependency prefetch service is not configured")
	}
	return nil
}

func normalizeOptions(opts ecosystem.PrefetchOptions) ecosystem.PrefetchOptions {
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = defaultMaxRecords
	}
	if opts.MinPullCount <= 0 {
		opts.MinPullCount = defaultMinPullCount
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.MaxTasks < 0 {
		opts.MaxTasks = 0
	}
	if opts.MaxRepositories < 0 {
		opts.MaxRepositories = 0
	}
	if opts.FailureBackoff < 0 {
		opts.FailureBackoff = 0
	}
	if opts.RetryWindow < 0 {
		opts.RetryWindow = 0
	}
	return opts
}

func rawAlias(ecosystemName, alias string) (string, bool) {
	prefix := strings.TrimSpace(ecosystemName) + "/"
	if !strings.HasPrefix(alias, prefix) {
		return "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(alias, prefix))
	return raw, raw != ""
}

func groupKey(candidate Candidate) string {
	return candidate.ScopedAlias + "\x1f" + candidate.Repository
}

func (s *Service) repositories(candidates *collectionlist.List[Candidate]) int {
	if candidates == nil {
		return 0
	}
	return len(lo.Uniq(collectionlist.MapList(candidates, func(_ int, candidate Candidate) string {
		return groupKey(candidate)
	}).Values()))
}

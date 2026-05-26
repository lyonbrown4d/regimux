package prefetch

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
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

func NewService(metadata meta.Store, tags cache.TagService, manifests cache.ManifestService, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		metadata:  metadata,
		tags:      tags,
		manifests: manifests,
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
	failed := 0
	for _, candidate := range candidates {
		_, err := s.manifests.Get(ctx, cache.ManifestRequest{
			UpstreamAlias:  candidate.Alias,
			Repo:           candidate.Repo,
			Reference:      candidate.Tag,
			Accept:         opts.Accept,
			Method:         http.MethodGet,
			SkipPullRecord: true,
		})
		if err != nil {
			failed++
			s.logger.WarnContext(ctx, "prefetch manifest failed",
				"alias", candidate.Alias,
				"repository", candidate.Repo,
				"reference", candidate.Tag,
				"reason", candidate.Reason,
				"score", candidate.Score,
				"error", err,
			)
			continue
		}
		prefetched = append(prefetched, candidate.Alias+"/"+candidate.Repo+":"+candidate.Tag)
		s.logger.InfoContext(ctx, "prefetched manifest",
			"alias", candidate.Alias,
			"repository", candidate.Repo,
			"reference", candidate.Tag,
			"reason", candidate.Reason,
			"score", candidate.Score,
		)
	}
	return prefetched, len(candidates), failed
}

func (s *Service) availableTags(ctx context.Context, route repoKey, pageSize int) ([]string, error) {
	result, err := s.tags.List(ctx, cache.TagRequest{
		UpstreamAlias: route.alias,
		Repo:          route.repo,
		N:             strconv.Itoa(pageSize),
	})
	if err != nil {
		return nil, err
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

func normalizeRunOptions(opts RunOptions) RunOptions {
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = defaultMaxRecords
	}
	if opts.TagsPageSize <= 0 {
		opts.TagsPageSize = defaultTagsPageSize
	}
	if opts.MinPullCount <= 0 {
		opts.MinPullCount = defaultMinPullCount
	}
	if opts.MaxCandidatesPerRepo <= 0 {
		opts.MaxCandidatesPerRepo = defaultMaxCandidates
	}
	if opts.MaxVersionDistance <= 0 {
		opts.MaxVersionDistance = defaultMaxVersionDistance
	}
	if opts.Accept == "" {
		opts.Accept = distribution.DefaultManifestAccept
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	return opts
}

func filterPullRecords(records []meta.PullRecord, opts RunOptions) []meta.PullRecord {
	out := make([]meta.PullRecord, 0, len(records))
	for _, record := range records {
		if record.Count < opts.MinPullCount || record.Reference == "" {
			continue
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastPullAt.Equal(out[j].LastPullAt) {
			return out[i].LastPullAt.After(out[j].LastPullAt)
		}
		return out[i].Count > out[j].Count
	})
	if len(out) > opts.MaxRecords {
		out = out[:opts.MaxRecords]
	}
	return out
}

func groupPullRecords(records []meta.PullRecord) map[repoKey][]meta.PullRecord {
	groups := make(map[repoKey][]meta.PullRecord)
	for _, record := range records {
		key := repoKey{alias: record.Alias, repo: record.Repository}
		groups[key] = append(groups[key], record)
	}
	return groups
}

func toCandidateRecords(records []meta.PullRecord) []PullRecord {
	out := make([]PullRecord, 0, len(records))
	for _, record := range records {
		count := record.Count
		if count > int64(^uint(0)>>1) {
			count = int64(^uint(0) >> 1)
		}
		out = append(out, PullRecord{
			Alias:      record.Alias,
			Repo:       record.Repository,
			Tag:        record.Reference,
			Count:      int(count),
			LastPullAt: record.LastPullAt,
		})
	}
	return out
}

func cacheError(message string) error {
	return oops.In("prefetch").Errorf("%s", message)
}

func cacheWrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("prefetch").Wrapf(err, "%s", message)
}

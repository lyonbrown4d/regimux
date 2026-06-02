package suggestion

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"golang.org/x/sync/singleflight"
)

// Service maintains lightweight tag indexes used to enrich manifest misses.
type Service struct {
	client TagClient
	cache  cacheStore
	logger *slog.Logger
	opts   Options
	group  singleflight.Group
}

func NewService(deps Dependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		client: deps.Client,
		cache:  deps.Cache,
		logger: logger.With("component", "suggestion"),
		opts:   normalizeOptions(deps.Options),
	}
}

func NewServiceFromParts(client upstream.RegistryClient, cache backend.Backend, opts Options, logger *slog.Logger) *Service {
	return NewService(Dependencies{
		Client:  client,
		Cache:   cache,
		Logger:  logger,
		Options: opts,
	})
}

func (s *Service) ObserveManifest(ctx context.Context, req ManifestRequest) {
	s.RecordManifestHit(ctx, req)
}

func (s *Service) SuggestManifest(ctx context.Context, req ManifestRequest) ManifestSuggestions {
	result := s.SuggestManifestMiss(ctx, req)
	return ManifestSuggestions{
		Tags:         result.Tags,
		Repositories: result.Repositories,
	}
}

// RecordManifestHit schedules a non-blocking refresh for the requested repo tag index.
func (s *Service) RecordManifestHit(ctx context.Context, req ManifestRequest) {
	if s == nil || s.opts.DisableAsyncRefresh || s.client == nil || s.cache == nil {
		return
	}
	req, ok := s.suggestionRequest(req)
	if !ok {
		return
	}

	go func() {
		refreshCtx := context.WithoutCancel(ctx)
		refreshCtx, cancel := context.WithTimeout(refreshCtx, s.opts.RefreshTimeout)
		defer cancel()
		if _, err := s.RefreshTags(refreshCtx, req); err != nil {
			s.logger.DebugContext(refreshCtx,
				"manifest hit tag suggestion refresh skipped",
				"alias", req.Alias,
				"repository", req.Repository,
				"reference", req.Reference,
				"error", err,
			)
		}
	}()
}

// SuggestManifestMiss returns tag suggestions from cache, or from one short refresh if cache is absent.
func (s *Service) SuggestManifestMiss(ctx context.Context, req ManifestRequest) ManifestMissResult {
	result := ManifestMissResult{Request: normalizeRequest(req), Source: SourceNone}
	if s == nil {
		return result
	}
	req, ok := s.suggestionRequest(req)
	if !ok {
		return result
	}
	result.Request = req

	index, ok, err := s.cachedTagIndex(ctx, req)
	if err != nil {
		result.CacheError = err
		s.logSuggestionSkip(ctx, req, "read manifest suggestion cache failed", err)
	} else if ok {
		result.Source = SourceCache
		return s.enrichTagSuggestions(ctx, req, result, index.Tags)
	}

	if s.negativeCached(ctx, req) {
		return s.enrichRepositorySuggestions(ctx, req, result)
	}

	if s.client == nil || s.cache == nil {
		return s.enrichRepositorySuggestions(ctx, req, result)
	}

	index, err = s.refreshWithTimeout(ctx, req)
	if err != nil {
		result.RefreshError = err
		s.logSuggestionSkip(ctx, req, "refresh manifest suggestion cache failed", err)
		return s.enrichRepositorySuggestions(ctx, req, result)
	}

	result.Source = SourceRefresh
	result.Refreshed = true
	return s.enrichTagSuggestions(ctx, req, result, index.Tags)
}

func (s *Service) refreshWithTimeout(ctx context.Context, req ManifestRequest) (*TagIndex, error) {
	refreshCtx, cancel := context.WithTimeout(ctx, s.opts.RefreshTimeout)
	defer cancel()
	return s.RefreshTags(refreshCtx, req)
}

func (s *Service) enrichTagSuggestions(
	ctx context.Context,
	req ManifestRequest,
	result ManifestMissResult,
	tags []string,
) ManifestMissResult {
	result.Tags = SuggestTags(req.Reference, tags, SuggestOptions{
		Limit: s.opts.MaxSuggestions,
	})
	result.Suggestions = manifestSuggestions(req.Alias, req.Repository, result.Tags)
	if len(result.Tags) > 0 {
		return result
	}
	return s.enrichRepositorySuggestions(ctx, req, result)
}

func (s *Service) enrichRepositorySuggestions(
	ctx context.Context,
	req ManifestRequest,
	result ManifestMissResult,
) ManifestMissResult {
	repositories, ok, err := s.cachedRepositories(ctx, req.Alias)
	if err != nil {
		if result.CacheError == nil {
			result.CacheError = err
		}
		s.logSuggestionSkip(ctx, req, "read repository suggestion cache failed", err)
		return result
	}
	if !ok {
		return result
	}
	result.Repositories = SuggestRepositories(req.Repository, repositories, SuggestOptions{
		Limit: s.opts.MaxSuggestions,
	})
	return result
}

// RefreshTags refreshes and stores the tag index for one upstream repository.
func (s *Service) RefreshTags(ctx context.Context, req ManifestRequest) (*TagIndex, error) {
	req, err := s.validateRefresh(req)
	if err != nil {
		return nil, err
	}

	value, err, _ := s.group.Do(tagIndexCacheKey(req), func() (any, error) {
		return s.refreshTagsOnce(ctx, req)
	})
	if err != nil {
		return nil, wrapError(err, "refresh manifest suggestion tags")
	}
	return tagIndexFromSingleflight(value)
}

func (s *Service) validateRefresh(req ManifestRequest) (ManifestRequest, error) {
	if s == nil {
		return ManifestRequest{}, errorf("suggestion service is nil")
	}
	req, err := validateRequest(req)
	if err != nil {
		return ManifestRequest{}, err
	}
	if s.client == nil {
		return ManifestRequest{}, errorf("tag client is required")
	}
	if s.cache == nil {
		return ManifestRequest{}, errorf("cache backend is required")
	}
	return req, nil
}

func (s *Service) refreshTagsOnce(ctx context.Context, req ManifestRequest) (*TagIndex, error) {
	index, err := s.fetchTagIndex(ctx, req)
	if err != nil {
		s.rememberNegativeTag(ctx, req, err)
		return nil, err
	}
	if err := s.storeTagIndex(ctx, index); err != nil {
		return nil, err
	}
	if err := s.rememberRepository(ctx, req); err != nil {
		s.logSuggestionSkip(ctx, req, "remember repository for manifest suggestions failed", err)
	}
	return index, nil
}

func (s *Service) rememberNegativeTag(ctx context.Context, req ManifestRequest, err error) {
	if !isDistributionNotFound(err) {
		return
	}
	if negativeErr := s.storeNegativeTag(ctx, req); negativeErr != nil {
		s.logSuggestionSkip(ctx, req, "store negative tag suggestion cache failed", negativeErr)
	}
}

func tagIndexFromSingleflight(value any) (*TagIndex, error) {
	index, ok := value.(*TagIndex)
	if !ok {
		return nil, errorf("unexpected manifest suggestion tag index type %T", value)
	}
	return index, nil
}

func (s *Service) suggestionRequest(req ManifestRequest) (ManifestRequest, bool) {
	req, err := validateRequest(req)
	if err != nil {
		return ManifestRequest{}, false
	}
	if reference.IsDigest(req.Reference) {
		return ManifestRequest{}, false
	}
	return req, true
}

func (s *Service) logSuggestionSkip(ctx context.Context, req ManifestRequest, message string, err error) {
	if s == nil || s.logger == nil || err == nil {
		return
	}
	s.logger.DebugContext(ctx,
		message,
		"alias", req.Alias,
		"repository", req.Repository,
		"reference", req.Reference,
		"error", err,
	)
}

func validateRequest(req ManifestRequest) (ManifestRequest, error) {
	req = normalizeRequest(req)
	if req.Alias == "" {
		return ManifestRequest{}, errorf("upstream alias is required")
	}
	if req.Repository == "" {
		return ManifestRequest{}, errorf("repository is required")
	}
	if req.Reference == "" {
		return ManifestRequest{}, errorf("manifest reference is required")
	}
	return req, nil
}

func isDistributionNotFound(err error) bool {
	list := distribution.FromError(err)
	if list == nil {
		return false
	}
	for _, item := range list.Errors {
		if item.Code == distribution.CodeManifestUnknown || item.Code == distribution.CodeNameUnknown {
			return true
		}
	}
	return false
}

func normalizeRequest(req ManifestRequest) ManifestRequest {
	return ManifestRequest{
		Alias:      strings.TrimSpace(req.Alias),
		Repository: strings.Trim(strings.TrimSpace(req.Repository), "/"),
		Reference:  strings.TrimSpace(req.Reference),
	}
}

func utcNow() time.Time {
	return time.Now().UTC()
}

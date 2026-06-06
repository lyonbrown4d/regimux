// Package suggestion maintains cached hint indexes for registry errors.
package suggestion

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionset "github.com/arcgolabs/collectionx/set"
)

type cacheStore interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

type tagIndexEnvelope struct {
	Version    int       `json:"version"`
	Alias      string    `json:"alias"`
	Repository string    `json:"repository"`
	Tags       []string  `json:"tags"`
	FetchedAt  time.Time `json:"fetched_at"`
}

type repositoryIndexEnvelope struct {
	Version      int       `json:"version"`
	Alias        string    `json:"alias"`
	Repositories []string  `json:"repositories"`
	FetchedAt    time.Time `json:"fetched_at"`
}

type negativeTagEnvelope struct {
	Version   int       `json:"version"`
	Alias     string    `json:"alias"`
	Repo      string    `json:"repo"`
	FetchedAt time.Time `json:"fetched_at"`
}

func (s *Service) cachedTagIndex(ctx context.Context, req ManifestRequest) (*TagIndex, bool, error) {
	if s == nil || s.cache == nil {
		return nil, false, nil
	}
	data, ok, err := s.cache.Get(ctx, tagIndexCacheKey(req))
	if err != nil {
		return nil, false, wrapError(err, "get manifest suggestion tag cache")
	}
	if !ok {
		return nil, false, nil
	}
	index, err := tagIndexFromCache(data)
	if err != nil {
		if deleteErr := s.cache.Delete(ctx, tagIndexCacheKey(req)); deleteErr != nil {
			return nil, false, wrapError(deleteErr, "delete invalid manifest suggestion tag cache")
		}
		return nil, false, err
	}
	return index, true, nil
}

func (s *Service) storeTagIndex(ctx context.Context, index *TagIndex) error {
	if s == nil || s.cache == nil || index == nil || s.opts.TagTTL < 0 {
		return nil
	}
	data, err := tagIndexToCache(index)
	if err != nil {
		return err
	}
	if err := s.cache.Set(ctx, tagIndexCacheKey(ManifestRequest{
		Alias:      index.Alias,
		Repository: index.Repository,
		Reference:  "tags",
	}), data, s.opts.TagTTL); err != nil {
		return wrapError(err, "set manifest suggestion tag cache")
	}
	if err := s.cache.Delete(ctx, negativeTagCacheKey(ManifestRequest{
		Alias:      index.Alias,
		Repository: index.Repository,
		Reference:  "tags",
	})); err != nil {
		s.logSuggestionSkip(ctx, normalizeRequest(ManifestRequest{
			Alias:      index.Alias,
			Repository: index.Repository,
			Reference:  "tags",
		}), "delete negative tag suggestion cache failed", err)
	}
	return nil
}

func (s *Service) negativeCached(ctx context.Context, req ManifestRequest) bool {
	if s == nil || s.cache == nil {
		return false
	}
	_, ok, err := s.cache.Get(ctx, negativeTagCacheKey(req))
	if err != nil {
		s.logSuggestionSkip(ctx, req, "read negative tag suggestion cache failed", err)
		return false
	}
	return ok
}

func (s *Service) storeNegativeTag(ctx context.Context, req ManifestRequest) error {
	if s == nil || s.cache == nil || s.opts.NegativeTTL < 0 {
		return nil
	}
	req = normalizeRequest(req)
	envelope := negativeTagEnvelope{
		Version:   defaultCacheKeyVersion,
		Alias:     req.Alias,
		Repo:      req.Repository,
		FetchedAt: utcNow(),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return wrapError(err, "marshal negative tag suggestion cache")
	}
	if err := s.cache.Set(ctx, negativeTagCacheKey(req), data, s.opts.NegativeTTL); err != nil {
		return wrapError(err, "set negative tag suggestion cache")
	}
	return nil
}

func (s *Service) cachedRepositories(ctx context.Context, alias string) ([]string, bool, error) {
	if s == nil || s.cache == nil {
		return nil, false, nil
	}
	data, ok, err := s.cache.Get(ctx, repositoryIndexCacheKey(alias))
	if err != nil {
		return nil, false, wrapError(err, "get repository suggestion cache")
	}
	if !ok {
		return nil, false, nil
	}
	index, err := repositoryIndexFromCache(data)
	if err != nil {
		if deleteErr := s.cache.Delete(ctx, repositoryIndexCacheKey(alias)); deleteErr != nil {
			return nil, false, wrapError(deleteErr, "delete invalid repository suggestion cache")
		}
		return nil, false, err
	}
	return index.Repositories, true, nil
}

func (s *Service) rememberRepository(ctx context.Context, req ManifestRequest) error {
	if s == nil || s.cache == nil || s.opts.RepositoryTTL < 0 {
		return nil
	}
	req, err := validateRequest(req)
	if err != nil {
		return err
	}
	repositories, _, err := s.cachedRepositories(ctx, req.Alias)
	if err != nil {
		return err
	}
	index := &repositoryIndex{
		Alias:        req.Alias,
		Repositories: normalizeRepositories(append(repositories, req.Repository), s.opts.RepositoryLimit),
		FetchedAt:    utcNow(),
	}
	data, err := repositoryIndexToCache(index)
	if err != nil {
		return err
	}
	if err := s.cache.Set(ctx, repositoryIndexCacheKey(req.Alias), data, s.opts.RepositoryTTL); err != nil {
		return wrapError(err, "set repository suggestion cache")
	}
	return nil
}

func tagIndexToCache(index *TagIndex) ([]byte, error) {
	envelope := tagIndexEnvelope{
		Version:    defaultCacheKeyVersion,
		Alias:      index.Alias,
		Repository: index.Repository,
		Tags:       normalizeTags(index.Tags),
		FetchedAt:  index.FetchedAt,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, wrapError(err, "marshal manifest suggestion tag cache")
	}
	return data, nil
}

func tagIndexFromCache(data []byte) (*TagIndex, error) {
	var envelope tagIndexEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, wrapError(err, "unmarshal manifest suggestion tag cache")
	}
	return &TagIndex{
		Alias:      strings.TrimSpace(envelope.Alias),
		Repository: strings.Trim(strings.TrimSpace(envelope.Repository), "/"),
		Tags:       normalizeTags(envelope.Tags),
		FetchedAt:  envelope.FetchedAt,
	}, nil
}

func repositoryIndexToCache(index *repositoryIndex) ([]byte, error) {
	envelope := repositoryIndexEnvelope{
		Version:      defaultCacheKeyVersion,
		Alias:        strings.TrimSpace(index.Alias),
		Repositories: normalizeRepositories(index.Repositories, 0),
		FetchedAt:    index.FetchedAt,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, wrapError(err, "marshal repository suggestion cache")
	}
	return data, nil
}

func repositoryIndexFromCache(data []byte) (*repositoryIndex, error) {
	var envelope repositoryIndexEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, wrapError(err, "unmarshal repository suggestion cache")
	}
	return &repositoryIndex{
		Alias:        strings.TrimSpace(envelope.Alias),
		Repositories: normalizeRepositories(envelope.Repositories, 0),
		FetchedAt:    envelope.FetchedAt,
	}, nil
}

func tagIndexCacheKey(req ManifestRequest) string {
	req = normalizeRequest(req)
	return cacheKey("tags", req.Alias, req.Repository)
}

func negativeTagCacheKey(req ManifestRequest) string {
	req = normalizeRequest(req)
	return cacheKey("tags-negative", req.Alias, req.Repository)
}

func repositoryIndexCacheKey(alias string) string {
	return cacheKey("repos", strings.TrimSpace(alias))
}

func cacheKey(parts ...string) string {
	clean := collectionlist.FilterMapList(collectionlist.NewList(parts...), func(_ int, part string) (string, bool) {
		part = strings.Trim(strings.TrimSpace(part), ":")
		return part, part != ""
	})
	return strings.Join(append([]string{"suggestion", "v1"}, clean.Values()...), ":")
}

func normalizeRepositories(repositories []string, limit int) []string {
	seen := collectionset.NewOrderedSetWithCapacity[string](len(repositories))
	collectionlist.NewList(repositories...).Range(func(_ int, repository string) bool {
		repository = strings.Trim(strings.TrimSpace(repository), "/")
		if repository == "" {
			return true
		}
		seen.Add(repository)
		return limit <= 0 || seen.Len() < limit
	})
	return seen.Values()
}

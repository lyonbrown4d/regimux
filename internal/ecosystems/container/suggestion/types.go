package suggestion

import (
	"context"
	"log/slog"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

const (
	defaultCacheKeyVersion = 1
	cacheSourceNone        = Source("none")
	cacheSourceCached      = Source("cache")
	cacheSourceRefreshed   = Source("refresh")
)

// TagClient is the upstream API surface needed by the suggestion service.
type TagClient interface {
	ListTags(ctx context.Context, req upstream.ListTagsRequest) (*upstream.TagsResponse, error)
}

// Dependencies contains the collaborators for Service.
type Dependencies struct {
	Client  TagClient
	Cache   backend.Backend
	Logger  *slog.Logger
	Options Options
}

// ManifestService is the endpoint-facing suggestion contract.
type ManifestService interface {
	ObserveManifest(ctx context.Context, req ManifestRequest)
	SuggestManifest(ctx context.Context, req ManifestRequest) ManifestSuggestions
}

// ManifestRequest identifies the manifest reference being suggested for.
type ManifestRequest struct {
	Alias      string
	Repository string
	Reference  string
}

// ManifestSuggestions contains structured enrichment for MANIFEST_UNKNOWN.
type ManifestSuggestions struct {
	Tags         []string
	Repositories []string
}

func (s ManifestSuggestions) Empty() bool {
	return len(s.Tags) == 0 && len(s.Repositories) == 0
}

// Source describes where a suggestion result came from.
type Source string

const (
	SourceNone    = cacheSourceNone
	SourceCache   = cacheSourceCached
	SourceRefresh = cacheSourceRefreshed
)

// TagIndex is the cached tag index for one upstream repository.
type TagIndex struct {
	Alias      string    `json:"alias"`
	Repository string    `json:"repository"`
	Tags       []string  `json:"tags"`
	FetchedAt  time.Time `json:"fetched_at"`
}

// ManifestMissResult is safe to use as enrichment data for MANIFEST_UNKNOWN.
type ManifestMissResult struct {
	Request      ManifestRequest
	Suggestions  []distribution.ManifestSuggestion
	Tags         []string
	Repositories []string
	Source       Source
	Refreshed    bool
	CacheError   error
	RefreshError error
}

func (r ManifestMissResult) HasSuggestions() bool {
	return len(r.Suggestions) > 0 || len(r.Repositories) > 0
}

type tagListBody struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type repositoryIndex struct {
	Alias        string    `json:"alias"`
	Repositories []string  `json:"repositories"`
	FetchedAt    time.Time `json:"fetched_at"`
}

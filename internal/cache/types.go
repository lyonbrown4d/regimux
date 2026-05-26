package cache

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

type CacheStatus string

const (
	CacheBypass CacheStatus = "bypass"
	CacheHit    CacheStatus = "hit"
	CacheMiss   CacheStatus = "miss"
	CacheStale  CacheStatus = "stale"
)

type ManifestService interface {
	Get(ctx context.Context, req ManifestRequest) (*CachedManifest, error)
}

type BlobService interface {
	Get(ctx context.Context, req BlobRequest) (*BlobReadResult, error)
}

type TagService interface {
	List(ctx context.Context, req TagRequest) (*TagsResult, error)
}

type ReferrerService interface {
	Get(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error)
}

type ManifestRequest struct {
	UpstreamAlias string
	Repo          string
	Reference     string
	Accept        string
	Method        string
}

type CachedManifest struct {
	Digest    string
	MediaType string
	Size      int64
	Body      []byte
	Headers   http.Header
	Cache     CacheStatus
}

func (m CachedManifest) SizeString() string {
	return strconv.FormatInt(m.Size, 10)
}

type BlobRequest struct {
	UpstreamAlias string
	Repo          string
	Digest        string
	Range         *reference.HTTPRange
	Method        string
}

type BlobReadResult struct {
	Reader  io.ReadCloser
	Digest  string
	Size    int64
	Range   *reference.HTTPRange
	Status  int
	Headers http.Header
	Cache   CacheStatus
}

type TagRequest struct {
	UpstreamAlias string
	Repo          string
	N             string
	Last          string
}

type TagsResult struct {
	Body    []byte
	Headers http.Header
	Cache   CacheStatus
}

type ReferrerRequest struct {
	UpstreamAlias string
	Repo          string
	Digest        string
}

type ReferrersResult struct {
	Body      []byte
	MediaType string
	Headers   http.Header
	Cache     CacheStatus
}

func ValidateRouteParts(alias, repo string) error {
	if alias == "" {
		return errors.New("upstream alias is required")
	}
	if repo == "" {
		return errors.New("repository is required")
	}
	return nil
}

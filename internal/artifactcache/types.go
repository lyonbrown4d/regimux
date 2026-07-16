// Package artifactcache stores ecosystem artifacts in the shared metadata/object stores.
package artifactcache

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	HeaderMirrorCache = "X-Mirror-Cache"
	CacheHit          = "hit"
	CacheMiss         = "miss"
	CacheStale        = "stale"
)

type MetadataStore interface {
	meta.ManifestRepository
	meta.TagRepository
	meta.BlobRepository
	meta.RepoBlobRepository
	meta.PullRepository
	meta.EndpointHealthRepository
}

type Dependencies struct {
	Metadata MetadataStore
	Objects  object.Store
	Fills    *FillTracker
	Logger   *slog.Logger
	Now      func() time.Time
}

type Store struct {
	metadata MetadataStore
	objects  object.Store
	fills    *FillTracker
	logger   *slog.Logger
	now      func() time.Time
}

type Key struct {
	Alias      string
	Repository string
	Reference  string
}

type Entry struct {
	Digest  string
	Size    int64
	Headers http.Header
	Body    io.ReadCloser
	Expired bool
}

type BodyValidator func(io.ReaderAt, int64) error

type PutRequest struct {
	Key         Key
	AcceptKey   string
	Body        io.Reader
	Headers     http.Header
	ContentType string
	TTL         time.Duration
	Validator   BodyValidator
}

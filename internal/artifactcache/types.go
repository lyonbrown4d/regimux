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

type Dependencies struct {
	Metadata meta.Store
	Objects  object.Store
	Logger   *slog.Logger
	Now      func() time.Time
}

type Store struct {
	metadata meta.Store
	objects  object.Store
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

type PutRequest struct {
	Key         Key
	AcceptKey   string
	Body        io.Reader
	Headers     http.Header
	ContentType string
	TTL         time.Duration
}

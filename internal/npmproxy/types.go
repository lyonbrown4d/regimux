// Package npmproxy exposes a read-through npm registry proxy cache.
package npmproxy

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	ecosystemNPM = "npm"

	defaultMetadataTTL = 5 * time.Minute
	headerMirrorCache  = "X-Mirror-Cache"
	cacheHit           = "hit"
	cacheMiss          = "miss"
	cacheStale         = "stale"

	metadataRef  = "metadata"
	tarballMedia = "application/octet-stream"

	routeMetadata = "metadata"
	routeTarball  = "tarball"
	routeOther    = "other"
)

type ServiceDependencies struct {
	Config      config.Config
	Metadata    meta.Store
	Objects     object.Store
	Cache       *artifactcache.Store
	Logger      *slog.Logger
	MetadataTTL time.Duration
	Client      *http.Client
	Now         func() time.Time
}

type Service struct {
	cfg         config.Config
	cache       *artifactcache.Store
	client      *http.Client
	logger      *slog.Logger
	publicURL   string
	metadataTTL time.Duration
	now         func() time.Time
}

type Request struct {
	Alias        string
	Tail         string
	Method       string
	ProxyBaseURL string
	Query        string
}

type Response struct {
	Status      int
	Headers     http.Header
	Body        io.ReadCloser
	ContentType string
	Size        int64
	Cache       string
}

type Upstream struct {
	Alias  string
	Config config.UpstreamConfig
}

type route struct {
	Alias        string
	Tail         string
	UpstreamTail string
	Package      string
	Reference    string
	Kind         string
	Query        string
	MetadataTTL  time.Duration
}

type upstreamFetch struct {
	status     int
	headers    http.Header
	body       io.ReadCloser
	requestURL string
}

type storedResponse struct {
	digest  string
	size    int64
	headers http.Header
	body    io.ReadCloser
	expired bool
}

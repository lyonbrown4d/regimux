// Package npm exposes a read-through npm registry proxy cache.
package npm

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	ecosystemNPM = "npm"

	defaultMetadataTTL = 5 * time.Minute
	headerMirrorCache  = artifactcache.HeaderMirrorCache
	cacheHit           = artifactcache.CacheHit
	cacheMiss          = artifactcache.CacheMiss
	cacheStale         = artifactcache.CacheStale

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
	Factory     *clientfactory.Factory
	Now         func() time.Time
	Events      events.Bus
}

type Service struct {
	cfg         config.Config
	metadata    meta.Store
	cache       *artifactcache.Store
	client      *http.Client
	factory     *clientfactory.Factory
	logger      *slog.Logger
	publicURL   string
	metadataTTL time.Duration
	now         func() time.Time
	events      events.Bus
}

type Request struct {
	Alias          string
	Tail           string
	Method         string
	ProxyBaseURL   string
	Query          string
	SkipPullRecord bool
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

type requestMode int

const (
	requestModeClient requestMode = iota
	requestModeRefresh
)

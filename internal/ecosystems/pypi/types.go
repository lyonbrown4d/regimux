// Package pypi exposes a read-through PyPI proxy cache.
package pypi

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
	ecosystemPyPI       = "pypi"
	defaultSimpleTTL    = 5 * time.Minute
	headerMirrorCache   = artifactcache.HeaderMirrorCache
	cacheHit            = artifactcache.CacheHit
	cacheMiss           = artifactcache.CacheMiss
	cacheStale          = artifactcache.CacheStale
	acceptKeyPyPI       = "pypi"
	defaultUserAgent    = "regimux/dev"
	defaultObjectPrefix = "regimux-pypi-*"
)

type ServiceDependencies struct {
	Config   config.Config
	Cache    *artifactcache.Store
	Metadata meta.Store
	Objects  object.Store
	Client   *http.Client
	Factory  *clientfactory.Factory
	Logger   *slog.Logger
	Now      func() time.Time
	Events   events.Bus
}

type Service struct {
	cfg       config.Config
	cache     *artifactcache.Store
	metadata  meta.Store
	client    *http.Client
	factory   *clientfactory.Factory
	logger    *slog.Logger
	publicURL string
	now       func() time.Time
	events    events.Bus
}

type Request struct {
	Alias          string
	Tail           string
	Query          string
	Method         string
	PublicURL      string
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

type RouteKind string

const (
	RouteSimple  RouteKind = "simple"
	RoutePackage RouteKind = "package"
)

type Route struct {
	Alias             string
	Kind              RouteKind
	Tail              string
	UpstreamTail      string
	Project           string
	NormalizedProject string
	PackageTail       string
	Query             string
	Repository        string
	Reference         string
	DirectURL         string
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

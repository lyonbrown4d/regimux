package dist

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
	defaultMetadataTTL = 24 * time.Hour
	defaultUserAgent   = "regimux/dev"

	headerMirrorCache = artifactcache.HeaderMirrorCache
	cacheHit          = artifactcache.CacheHit
	cacheMiss         = artifactcache.CacheMiss
	cacheStale        = artifactcache.CacheStale
	acceptKeyDist     = "dist"
)

type ServiceDependencies struct {
	Config   config.Config
	Metadata meta.Store
	Objects  object.Store
	Cache    *artifactcache.Store
	Client   *http.Client
	Factory  *clientfactory.Factory
	Logger   *slog.Logger
	Events   events.Bus
	Now      func() time.Time
}

type Service struct {
	cfg      config.Config
	metadata meta.Store
	objects  object.Store
	cache    *artifactcache.Store
	client   *http.Client
	factory  *clientfactory.Factory
	logger   *slog.Logger
	now      func() time.Time
}

type Request struct {
	Alias          string
	Tail           string
	Method         string
	Range          string
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

type Route struct {
	Alias        string
	Tail         string
	Repository   string
	Reference    string
	UpstreamTail string
}

type Upstream struct {
	Alias  string
	Config config.UpstreamConfig
	Allow  []string
}

type upstreamFetch struct {
	status  int
	headers http.Header
	body    io.ReadCloser
}

type storedResponse struct {
	digest  string
	size    int64
	headers http.Header
	body    io.ReadCloser
	expired bool
	status  int
}

type requestMode int

const (
	requestModeClient requestMode = iota
	requestModeRefresh
)

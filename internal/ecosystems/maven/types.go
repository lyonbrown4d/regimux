// Package maven exposes a read-through Maven repository proxy cache.
package maven

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
	ecosystemMaven      = "maven"
	defaultMetadataTTL  = 5 * time.Minute
	headerMirrorCache   = "X-Mirror-Cache"
	cacheHit            = "hit"
	cacheMiss           = "miss"
	cacheStale          = "stale"
	acceptKeyMavenProxy = "maven-proxy"
	defaultUserAgent    = "regimux/dev"
	defaultObjectPrefix = "regimux-maven-proxy-*"
)

type ServiceDependencies struct {
	Config   config.Config
	Cache    *artifactcache.Store
	Metadata meta.Store
	Objects  object.Store
	Client   *http.Client
	Logger   *slog.Logger
	Now      func() time.Time
}

type Service struct {
	cfg      config.Config
	cache    *artifactcache.Store
	metadata meta.Store
	client   *http.Client
	logger   *slog.Logger
	now      func() time.Time
}

type Request struct {
	Alias          string
	Tail           string
	Query          string
	Method         string
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
	RouteMetadata RouteKind = "metadata"
	RouteSnapshot RouteKind = "snapshot"
	RouteRelease  RouteKind = "release"
)

type Route struct {
	Alias        string
	Kind         RouteKind
	Tail         string
	UpstreamTail string
	Repository   string
	Reference    string
	Query        string
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
}

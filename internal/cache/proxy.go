// Package cache coordinates registry response caching.
package cache

import (
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"golang.org/x/sync/singleflight"
)

type Proxy struct {
	client               upstream.RegistryClient
	cache                backend.Backend
	metadata             meta.Store
	objects              object.Store
	manifestTTL          time.Duration
	manifestStaleIfError bool
	manifestMaxStale     time.Duration
	tagsTTL              time.Duration
	referrersTTL         time.Duration
	referrersFallbackTag bool
	events               events.Bus
	blobStreamAndCache   bool
	blobVerifyTTL        time.Duration
	blobSmallCache       smallBlobCacheConfig
	logger               *slog.Logger
	manifestGroup        singleflight.Group
	blobGroup            singleflight.Group
}

type ProxyDependencies struct {
	Client      upstream.RegistryClient
	Cache       backend.Backend
	Metadata    meta.Store
	Objects     object.Store
	CacheConfig config.CacheConfig
	Events      events.Bus
	Logger      *slog.Logger
}

func NewProxy(deps ProxyDependencies) *Proxy {
	p := &Proxy{
		client:               deps.Client,
		cache:                deps.Cache,
		metadata:             deps.Metadata,
		objects:              deps.Objects,
		events:               deps.Events,
		logger:               deps.Logger,
		manifestTTL:          defaultManifestTTL(),
		manifestMaxStale:     168 * time.Hour,
		blobVerifyTTL:        deps.CacheConfig.Blob.VerifyTTL,
		tagsTTL:              5 * time.Minute,
		referrersTTL:         5 * time.Minute,
		manifestStaleIfError: deps.CacheConfig.Manifest.StaleIfError,
		referrersFallbackTag: deps.CacheConfig.Referrers.FallbackTag,
		blobStreamAndCache:   deps.CacheConfig.Blob.StreamAndCache,
		blobSmallCache: smallBlobCacheConfig{
			enabled:      deps.CacheConfig.Blob.SmallCache.Enabled,
			maxSizeBytes: deps.CacheConfig.Blob.SmallCache.MaxSizeBytes,
			ttl:          deps.CacheConfig.Blob.SmallCache.TTL,
		},
	}
	if deps.CacheConfig.Manifest.TagTTL > 0 {
		p.manifestTTL = deps.CacheConfig.Manifest.TagTTL
	}
	if deps.CacheConfig.Manifest.MaxStale > 0 {
		p.manifestMaxStale = deps.CacheConfig.Manifest.MaxStale
	}
	if deps.CacheConfig.Tags.TTL > 0 {
		p.tagsTTL = deps.CacheConfig.Tags.TTL
	}
	if deps.CacheConfig.Referrers.TTL > 0 {
		p.referrersTTL = deps.CacheConfig.Referrers.TTL
	}
	if p.cache == nil {
		p.cache = backend.Noop{}
	}
	if p.logger == nil {
		p.logger = slog.Default()
	}
	p.logger = p.logger.With("component", "cache.proxy")
	p.logger.Info("cache proxy configured",
		"manifest_ttl", p.manifestTTL,
		"manifest_stale_if_error", p.manifestStaleIfError,
		"manifest_max_stale", p.manifestMaxStale,
		"tags_ttl", p.tagsTTL,
		"referrers_ttl", p.referrersTTL,
		"blob_stream_and_cache", p.blobStreamAndCache,
		"blob_verify_ttl", p.blobVerifyTTL,
		"blob_small_cache_enabled", p.blobSmallCache.enabled,
		"blob_small_cache_max_size_bytes", p.blobSmallCache.maxSizeBytes,
		"blob_small_cache_ttl", p.blobSmallCache.ttl,
	)
	return p
}

func (p *Proxy) Manifests() ManifestService {
	return manifestProxy{
		client:       p.client,
		cache:        p.cache,
		metadata:     p.metadata,
		objects:      p.objects,
		events:       p.events,
		ttl:          p.manifestTTL,
		staleIfError: p.manifestStaleIfError,
		maxStale:     p.manifestMaxStale,
		group:        &p.manifestGroup,
	}
}

func (p *Proxy) Blobs() BlobService {
	return blobProxy{
		client:           p.client,
		cache:            p.cache,
		metadata:         p.metadata,
		objects:          p.objects,
		events:           p.events,
		logger:           p.logger,
		streamAndCache:   p.blobStreamAndCache,
		verifyMembership: p.blobVerifyTTL,
		smallCache:       p.blobSmallCache,
		group:            &p.blobGroup,
	}
}

func (p *Proxy) Tags() TagService {
	return tagProxy{
		client: p.client,
		cache:  p.cache,
		events: p.events,
		ttl:    p.tagsTTL,
	}
}

func (p *Proxy) Referrers() ReferrerService {
	return referrerProxy{
		client:      p.client,
		cache:       p.cache,
		events:      p.events,
		ttl:         p.referrersTTL,
		fallbackTag: p.referrersFallbackTag,
	}
}

type manifestProxy struct {
	client       upstream.RegistryClient
	cache        backend.Backend
	metadata     meta.Store
	objects      object.Store
	events       events.Bus
	ttl          time.Duration
	staleIfError bool
	maxStale     time.Duration
	group        *singleflight.Group
}

type blobProxy struct {
	client           upstream.RegistryClient
	cache            backend.Backend
	metadata         meta.Store
	objects          object.Store
	events           events.Bus
	logger           *slog.Logger
	streamAndCache   bool
	verifyMembership time.Duration
	smallCache       smallBlobCacheConfig
	group            *singleflight.Group
}

type tagProxy struct {
	client upstream.RegistryClient
	cache  backend.Backend
	events events.Bus
	ttl    time.Duration
}

type referrerProxy struct {
	client      upstream.RegistryClient
	cache       backend.Backend
	events      events.Bus
	ttl         time.Duration
	fallbackTag bool
}

func defaultManifestTTL() time.Duration {
	return 10 * time.Minute
}

func readHTTPBody(body io.ReadCloser, label string) ([]byte, error) {
	if body == nil {
		return nil, nil
	}

	data, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil {
		return nil, wrapError(readErr, "read %s", label)
	}
	if closeErr != nil {
		return nil, wrapError(closeErr, "close %s", label)
	}
	return data, nil
}

func closeHTTPBody(body io.Closer, label string) error {
	if body == nil {
		return nil
	}
	if err := body.Close(); err != nil {
		return wrapError(err, "close %s", label)
	}
	return nil
}

func contentTypeFromHeader(headers http.Header) string {
	value := headers.Get(distribution.HeaderContentType)
	if value == "" {
		return distribution.MediaTypeOctetStream
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil && mediaType != "" {
		return mediaType
	}
	return value
}

func rewriteTagsHeaders(headers http.Header, req TagRequest) http.Header {
	out := headers.Clone()
	link := out.Get(distribution.HeaderLink)
	if link == "" {
		return out
	}
	out.Set(distribution.HeaderLink, rewriteLinkHeader(link, req.UpstreamAlias, req.Repo))
	return out
}

func rewriteLinkHeader(header, alias, repo string) string {
	return collectionlist.MapList(collectionlist.NewList(strings.Split(header, ",")...), func(_ int, part string) string {
		return rewriteLinkPart(part, alias, repo)
	}).Join(",")
}

func rewriteLinkPart(part, alias, repo string) string {
	left := strings.Index(part, "<")
	right := strings.Index(part, ">")
	if left < 0 || right <= left {
		return part
	}

	raw := strings.TrimSpace(part[left+1 : right])
	parsed, err := url.Parse(raw)
	if err != nil {
		return part
	}
	next := &url.URL{
		Path:     "/v2/" + strings.Trim(alias, "/") + "/" + strings.Trim(repo, "/") + "/tags/list",
		RawQuery: parsed.RawQuery,
	}
	return part[:left+1] + next.String() + part[right:]
}

var (
	_ ManifestService = manifestProxy{}
	_ BlobService     = blobProxy{}
	_ TagService      = tagProxy{}
	_ ReferrerService = referrerProxy{}
)

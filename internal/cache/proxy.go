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
	logger               *slog.Logger
	manifestGroup        singleflight.Group
	blobGroup            singleflight.Group
}

type ProxyDependencies struct {
	Client               upstream.RegistryClient
	Cache                backend.Backend
	Metadata             meta.Store
	Objects              object.Store
	Config               config.Config
	Events               events.Bus
	Logger               *slog.Logger
}

func NewProxy(deps ProxyDependencies) *Proxy {
	p := &Proxy{
		client:              deps.Client,
		cache:               deps.Cache,
		metadata:            deps.Metadata,
		objects:             deps.Objects,
		events:              deps.Events,
		logger:              deps.Logger,
		manifestTTL:         defaultManifestTTL(),
		manifestMaxStale:    168 * time.Hour,
		blobVerifyTTL:       deps.Config.Cache.Blob.VerifyTTL,
		tagsTTL:             5 * time.Minute,
		referrersTTL:        5 * time.Minute,
		manifestStaleIfError: deps.Config.Cache.Manifest.StaleIfError,
		referrersFallbackTag: deps.Config.Cache.Referrers.FallbackTag,
		blobStreamAndCache:  deps.Config.Cache.Blob.StreamAndCache,
	}
	if deps.Config.Cache.Manifest.TagTTL > 0 {
		p.manifestTTL = deps.Config.Cache.Manifest.TagTTL
	}
	if deps.Config.Cache.Manifest.MaxStale > 0 {
		p.manifestMaxStale = deps.Config.Cache.Manifest.MaxStale
	}
	if deps.Config.Cache.Tags.TTL > 0 {
		p.tagsTTL = deps.Config.Cache.Tags.TTL
	}
	if deps.Config.Cache.Referrers.TTL > 0 {
		p.referrersTTL = deps.Config.Cache.Referrers.TTL
	}
	if p.cache == nil {
		p.cache = backend.Noop{}
	}
	if p.logger == nil {
		p.logger = slog.Default()
	}
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
		metadata:         p.metadata,
		objects:          p.objects,
		events:           p.events,
		logger:           p.logger,
		streamAndCache:   p.blobStreamAndCache,
		verifyMembership:  p.blobVerifyTTL,
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
	client          upstream.RegistryClient
	metadata        meta.Store
	objects         object.Store
	events          events.Bus
	logger          *slog.Logger
	streamAndCache  bool
	verifyMembership time.Duration
	group           *singleflight.Group
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
	parts := strings.Split(header, ",")
	for i, part := range parts {
		parts[i] = rewriteLinkPart(part, alias, repo)
	}
	return strings.Join(parts, ",")
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

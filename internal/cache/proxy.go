// Package cache coordinates registry response caching.
package cache

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
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
	manifestGroup        singleflight.Group
	blobGroup            singleflight.Group
}

type Options struct {
	Cache                backend.Backend
	Metadata             meta.Store
	Objects              object.Store
	ManifestTTL          time.Duration
	ManifestStaleIfError bool
	ManifestMaxStale     time.Duration
	TagsTTL              time.Duration
	ReferrersTTL         time.Duration
	ReferrersFallbackTag bool
}

type Option func(*Proxy)

func WithBackend(cache backend.Backend) Option {
	return func(p *Proxy) {
		p.cache = cache
	}
}

func WithMetadata(metadata meta.Store) Option {
	return func(p *Proxy) {
		p.metadata = metadata
	}
}

func WithObjects(objects object.Store) Option {
	return func(p *Proxy) {
		p.objects = objects
	}
}

func WithManifestTTL(ttl time.Duration) Option {
	return func(p *Proxy) {
		p.manifestTTL = ttl
	}
}

func WithManifestStaleIfError(enabled bool) Option {
	return func(p *Proxy) {
		p.manifestStaleIfError = enabled
	}
}

func WithManifestMaxStale(ttl time.Duration) Option {
	return func(p *Proxy) {
		p.manifestMaxStale = ttl
	}
}

func WithTagsTTL(ttl time.Duration) Option {
	return func(p *Proxy) {
		p.tagsTTL = ttl
	}
}

func WithReferrersTTL(ttl time.Duration) Option {
	return func(p *Proxy) {
		p.referrersTTL = ttl
	}
}

func WithReferrersFallbackTag(enabled bool) Option {
	return func(p *Proxy) {
		p.referrersFallbackTag = enabled
	}
}

func NewProxy(client upstream.RegistryClient, opts ...Option) *Proxy {
	p := &Proxy{
		client:           client,
		cache:            backend.Noop{},
		manifestTTL:      defaultManifestTTL(),
		manifestMaxStale: 168 * time.Hour,
		tagsTTL:          5 * time.Minute,
		referrersTTL:     5 * time.Minute,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	if p.cache == nil {
		p.cache = backend.Noop{}
	}
	return p
}

func (p *Proxy) Manifests() ManifestService {
	return manifestProxy{
		client:       p.client,
		cache:        p.cache,
		metadata:     p.metadata,
		objects:      p.objects,
		ttl:          p.manifestTTL,
		staleIfError: p.manifestStaleIfError,
		maxStale:     p.manifestMaxStale,
		group:        &p.manifestGroup,
	}
}

func (p *Proxy) Blobs() BlobService {
	return blobProxy{
		client:   p.client,
		metadata: p.metadata,
		objects:  p.objects,
		group:    &p.blobGroup,
	}
}

func (p *Proxy) Tags() TagService {
	return tagProxy{
		client: p.client,
		cache:  p.cache,
		ttl:    p.tagsTTL,
	}
}

func (p *Proxy) Referrers() ReferrerService {
	return referrerProxy{
		client:      p.client,
		cache:       p.cache,
		ttl:         p.referrersTTL,
		fallbackTag: p.referrersFallbackTag,
	}
}

type manifestProxy struct {
	client       upstream.RegistryClient
	cache        backend.Backend
	metadata     meta.Store
	objects      object.Store
	ttl          time.Duration
	staleIfError bool
	maxStale     time.Duration
	group        *singleflight.Group
}

type blobProxy struct {
	client   upstream.RegistryClient
	metadata meta.Store
	objects  object.Store
	group    *singleflight.Group
}

type tagProxy struct {
	client upstream.RegistryClient
	cache  backend.Backend
	ttl    time.Duration
}

type referrerProxy struct {
	client      upstream.RegistryClient
	cache       backend.Backend
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
		return nil, fmt.Errorf("read %s: %w", label, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close %s: %w", label, closeErr)
	}
	return data, nil
}

func closeHTTPBody(body io.Closer, label string) error {
	if body == nil {
		return nil
	}
	if err := body.Close(); err != nil {
		return fmt.Errorf("close %s: %w", label, err)
	}
	return nil
}

func contentTypeFromHeader(headers http.Header) string {
	value := headers.Get("Content-Type")
	if value == "" {
		return "application/octet-stream"
	}
	if before, _, ok := strings.Cut(value, ";"); ok {
		return strings.TrimSpace(before)
	}
	return value
}

func rewriteTagsHeaders(headers http.Header, req TagRequest) http.Header {
	out := headers.Clone()
	link := out.Get("Link")
	if link == "" {
		return out
	}
	out.Set("Link", rewriteLinkHeader(link, req.UpstreamAlias, req.Repo))
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

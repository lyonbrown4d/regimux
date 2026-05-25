package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/reference"
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
		client:       client,
		cache:        backend.Noop{},
		manifestTTL:  10 * time.Minute,
		tagsTTL:      5 * time.Minute,
		referrersTTL: 5 * time.Minute,
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
		client:   p.client,
		cache:    p.cache,
		metadata: p.metadata,
		objects:  p.objects,
		ttl:      p.manifestTTL,
		group:    &p.manifestGroup,
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
	client   upstream.RegistryClient
	cache    backend.Backend
	metadata meta.Store
	objects  object.Store
	ttl      time.Duration
	group    *singleflight.Group
}

func (p manifestProxy) Get(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	if cached, ok, err := p.lookup(ctx, req); err != nil {
		return nil, err
	} else if ok {
		if req.Method == http.MethodHead {
			cached.Body = nil
		}
		return cached, nil
	}

	cacheKey := manifestCacheKey(req)
	if p.group == nil {
		result, err := p.fetch(ctx, req)
		if err != nil {
			return nil, err
		}
		p.store(ctx, req, cacheKey, result)
		return result, nil
	}
	value, err, _ := p.group.Do(cacheKey, func() (any, error) {
		if cached, ok, err := p.lookup(ctx, req); err != nil {
			return nil, err
		} else if ok {
			if req.Method == http.MethodHead {
				cached.Body = nil
			}
			return cached, nil
		}
		result, err := p.fetch(ctx, req)
		if err != nil {
			return nil, err
		}
		p.store(ctx, req, cacheKey, result)
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	result, ok := value.(*CachedManifest)
	if !ok {
		return nil, fmt.Errorf("unexpected manifest cache result type %T", value)
	}
	return result, nil
}

func (p manifestProxy) fetch(ctx context.Context, req ManifestRequest) (*CachedManifest, error) {
	resp, err := p.client.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Reference:     req.Reference,
		Accept:        req.Accept,
		Method:        req.Method,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var body []byte
	if req.Method != http.MethodHead {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read manifest body: %w", err)
		}
	}

	return &CachedManifest{
		Digest:    resp.Digest,
		MediaType: resp.MediaType,
		Size:      resp.Size,
		Body:      body,
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

func (p manifestProxy) lookup(ctx context.Context, req ManifestRequest) (*CachedManifest, bool, error) {
	cacheKey := manifestCacheKey(req)
	if p.cache != nil {
		data, ok, err := p.cache.Get(ctx, cacheKey)
		if err != nil {
			return nil, false, err
		}
		if ok {
			manifest, err := manifestFromEnvelope(data)
			if err != nil {
				_ = p.cache.Delete(ctx, cacheKey)
				return nil, false, nil
			}
			manifest.Cache = CacheHit
			return manifest, true, nil
		}
	}
	if p.metadata == nil || p.objects == nil {
		return nil, false, nil
	}
	record, ok, err := p.lookupMetadata(ctx, req)
	if err != nil || !ok {
		return nil, false, err
	}
	var body []byte
	if req.Method != http.MethodHead {
		reader, _, err := p.objects.Get(ctx, record.Digest, object.GetOptions{})
		if err != nil {
			if errors.Is(err, object.ErrNotFound) {
				return nil, false, nil
			}
			return nil, false, err
		}
		defer reader.Close()
		body, err = io.ReadAll(reader)
		if err != nil {
			return nil, false, err
		}
	}
	manifest := &CachedManifest{
		Digest:    record.Digest,
		MediaType: record.MediaType,
		Size:      record.Size,
		Body:      body,
		Headers:   http.Header(record.Headers).Clone(),
		Cache:     CacheHit,
	}
	if p.cache != nil && len(body) > 0 {
		if data, err := manifestEnvelopeFromRecord(*record, body); err == nil {
			_ = p.cache.Set(ctx, cacheKey, data, ttlUntil(record.ExpiresAt, p.ttl))
		}
	}
	return manifest, true, nil
}

func (p manifestProxy) lookupMetadata(ctx context.Context, req ManifestRequest) (*meta.ManifestRecord, bool, error) {
	now := time.Now()
	acceptKey := reference.AcceptKey(req.Accept)

	if reference.IsDigest(req.Reference) {
		digest, _ := reference.NormalizeDigest(req.Reference)
		return p.lookupManifestRecord(ctx, meta.ManifestKey{
			Alias:      req.UpstreamAlias,
			Repository: req.Repo,
			Digest:     digest,
		}, acceptKey, now)
	}

	tag, ok, err := p.metadata.Tag(ctx, meta.TagKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
	})
	if err != nil || !ok {
		return nil, false, err
	}
	if !tag.ExpiresAt.IsZero() && !now.Before(tag.ExpiresAt) {
		return nil, false, nil
	}
	return p.lookupManifestRecord(ctx, meta.ManifestKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     tag.Digest,
	}, acceptKey, now)
}

func (p manifestProxy) lookupManifestRecord(ctx context.Context, key meta.ManifestKey, acceptKey string, now time.Time) (*meta.ManifestRecord, bool, error) {
	record, ok, err := p.metadata.Manifest(ctx, key)
	if err != nil || !ok {
		return nil, false, err
	}
	if record.Expired(now) {
		return nil, false, nil
	}
	if record.AcceptKey != "" && record.AcceptKey != acceptKey {
		return nil, false, nil
	}
	return record, true, nil
}

func (p manifestProxy) store(ctx context.Context, req ManifestRequest, cacheKey string, manifest *CachedManifest) {
	if manifest == nil {
		return
	}
	ttl := p.ttl
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	expiresAt := time.Now().UTC().Add(ttl)
	record := meta.ManifestRecord{
		Key:        cacheKey,
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
		AcceptKey:  reference.AcceptKey(req.Accept),
		Digest:     manifest.Digest,
		MediaType:  manifest.MediaType,
		Size:       manifest.Size,
		ObjectKey:  manifest.Digest,
		Headers:    map[string][]string(manifest.Headers.Clone()),
		ExpiresAt:  expiresAt,
	}
	objectStored := len(manifest.Body) == 0
	if p.objects != nil && manifest.Digest != "" && len(manifest.Body) > 0 {
		if info, err := p.objects.Put(ctx, manifest.Digest, bytes.NewReader(manifest.Body), object.PutOptions{ContentType: manifest.MediaType}); err == nil {
			record.ObjectKey = info.Digest
			if record.Size < 0 {
				record.Size = info.Size
			}
			objectStored = true
		}
	}
	if p.metadata != nil && manifest.Digest != "" && objectStored {
		if _, err := p.metadata.UpsertManifest(ctx, record); err == nil && !reference.IsDigest(req.Reference) {
			_, _ = p.metadata.UpsertTag(ctx, meta.TagRecord{
				Alias:      req.UpstreamAlias,
				Repository: req.Repo,
				Reference:  req.Reference,
				Digest:     manifest.Digest,
				ExpiresAt:  expiresAt,
			})
		}
	}
	if p.cache != nil {
		if data, err := manifestEnvelopeFromRecord(record, manifest.Body); err == nil {
			_ = p.cache.Set(ctx, cacheKey, data, ttl)
		}
	}
}

type blobProxy struct {
	client   upstream.RegistryClient
	metadata meta.Store
	objects  object.Store
	group    *singleflight.Group
}

func (p blobProxy) Get(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	if cached, ok, err := p.lookup(ctx, req); err != nil {
		return nil, err
	} else if ok {
		return cached, nil
	}

	if p.metadata == nil || p.objects == nil || req.Method == http.MethodHead {
		return p.fetchPassthrough(ctx, req)
	}

	key := "blob:" + req.Digest
	if p.group == nil {
		if err := p.fetchAndStore(ctx, req); err != nil {
			return nil, err
		}
		return p.openStored(ctx, req, CacheMiss)
	}
	_, err, _ := p.group.Do(key, func() (any, error) {
		if _, ok, err := p.lookup(ctx, BlobRequest{
			UpstreamAlias: req.UpstreamAlias,
			Repo:          req.Repo,
			Digest:        req.Digest,
			Method:        http.MethodHead,
		}); err != nil || ok {
			return nil, err
		}
		return nil, p.fetchAndStore(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	return p.openStored(ctx, req, CacheMiss)
}

func (p blobProxy) fetchPassthrough(ctx context.Context, req BlobRequest) (*BlobReadResult, error) {
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Range:         req.Range,
		Method:        req.Method,
	})
	if err != nil {
		return nil, err
	}

	reader := resp.Body
	if req.Method == http.MethodHead {
		_ = resp.Body.Close()
		reader = io.NopCloser(bytes.NewReader(nil))
	}
	return &BlobReadResult{
		Reader:  reader,
		Digest:  resp.Digest,
		Size:    resp.Size,
		Range:   req.Range,
		Status:  resp.StatusCode,
		Headers: resp.Headers,
		Cache:   CacheBypass,
	}, nil
}

func (p blobProxy) lookup(ctx context.Context, req BlobRequest) (*BlobReadResult, bool, error) {
	if p.metadata == nil || p.objects == nil {
		return nil, false, nil
	}
	if _, ok, err := p.metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
	}); err != nil {
		return nil, false, err
	} else if ok {
		result, err := p.openStored(ctx, req, CacheHit)
		if errors.Is(err, object.ErrNotFound) {
			return nil, false, nil
		}
		return result, err == nil, err
	}

	exists, err := p.objects.Exists(ctx, req.Digest)
	if err != nil || !exists {
		return nil, false, err
	}
	if err := p.verifyRepoBlob(ctx, req); err != nil {
		return nil, false, err
	}
	result, err := p.openStored(ctx, req, CacheHit)
	return result, err == nil, err
}

func (p blobProxy) verifyRepoBlob(ctx context.Context, req BlobRequest) error {
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        http.MethodHead,
	})
	if err != nil {
		return err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	if resp.Digest != "" && resp.Digest != req.Digest {
		return distribution.ErrDigestMismatch.WithDetail(map[string]string{
			"expected": req.Digest,
			"actual":   resp.Digest,
		})
	}
	stat, err := p.objects.Stat(ctx, req.Digest)
	if err != nil {
		return err
	}
	if err := p.upsertBlobRecords(ctx, req, stat, contentTypeFromHeader(resp.Headers)); err != nil {
		return err
	}
	return nil
}

func (p blobProxy) fetchAndStore(ctx context.Context, req BlobRequest) error {
	resp, err := p.client.GetBlob(ctx, upstream.GetBlobRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.Digest != "" && resp.Digest != req.Digest {
		return distribution.ErrDigestMismatch.WithDetail(map[string]string{
			"expected": req.Digest,
			"actual":   resp.Digest,
		})
	}
	info, err := p.objects.Put(ctx, req.Digest, resp.Body, object.PutOptions{ContentType: contentTypeFromHeader(resp.Headers)})
	if err != nil {
		if errors.Is(err, object.ErrDigestMismatch) {
			return distribution.ErrDigestMismatch.WithDetail(err.Error())
		}
		return err
	}
	return p.upsertBlobRecords(ctx, req, info, contentTypeFromHeader(resp.Headers))
}

func (p blobProxy) upsertBlobRecords(ctx context.Context, req BlobRequest, info *object.Info, mediaType string) error {
	if p.metadata == nil || info == nil {
		return nil
	}
	if _, err := p.metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       info.Digest,
		Size:         info.Size,
		MediaType:    mediaType,
		ObjectKey:    info.Digest,
		LastAccessAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	_, err := p.metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          req.UpstreamAlias,
		Repository:     req.Repo,
		Digest:         info.Digest,
		LastVerifiedAt: time.Now().UTC(),
	})
	return err
}

func (p blobProxy) openStored(ctx context.Context, req BlobRequest, cacheStatus CacheStatus) (*BlobReadResult, error) {
	info, err := p.objects.Stat(ctx, req.Digest)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Content-Length", strconv.FormatInt(info.Size, 10))
	headers.Set("ETag", info.ETag)

	status := http.StatusOK
	size := info.Size
	opts := object.GetOptions{}
	if req.Range != nil {
		resolved, err := req.Range.Resolve(info.Size)
		if err != nil {
			return nil, distribution.ErrRangeInvalid.WithDetail(err.Error())
		}
		status = http.StatusPartialContent
		size = resolved.Length()
		headers.Set("Content-Length", strconv.FormatInt(size, 10))
		headers.Set("Content-Range", resolved.ContentRange(info.Size))
		opts.Range = req.Range
	}

	reader := io.ReadCloser(io.NopCloser(bytes.NewReader(nil)))
	if req.Method != http.MethodHead {
		reader, info, err = p.objects.Get(ctx, req.Digest, opts)
		if err != nil {
			return nil, err
		}
		size = info.Size
	}
	return &BlobReadResult{
		Reader:  reader,
		Digest:  info.Digest,
		Size:    size,
		Range:   req.Range,
		Status:  status,
		Headers: headers,
		Cache:   cacheStatus,
	}, nil
}

type tagProxy struct {
	client upstream.RegistryClient
	cache  backend.Backend
	ttl    time.Duration
}

func (p tagProxy) List(ctx context.Context, req TagRequest) (*TagsResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	cacheKey := tagsCacheKey(req)
	if p.cache != nil && p.ttl > 0 {
		data, ok, err := p.cache.Get(ctx, cacheKey)
		if err != nil {
			return nil, err
		}
		if ok {
			result, err := tagsFromEnvelope(data)
			if err != nil {
				_ = p.cache.Delete(ctx, cacheKey)
			} else {
				result.Cache = CacheHit
				return result, nil
			}
		}
	}

	resp, err := p.client.ListTags(ctx, upstream.ListTagsRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		N:             req.N,
		Last:          req.Last,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tags body: %w", err)
	}
	headers := rewriteTagsHeaders(resp.Headers, req)
	result := &TagsResult{
		Body:    body,
		Headers: headers,
		Cache:   CacheBypass,
	}
	if p.cache != nil && p.ttl > 0 {
		if data, err := tagsEnvelopeFromResult(result); err == nil {
			_ = p.cache.Set(ctx, cacheKey, data, p.ttl)
		}
	}
	return result, nil
}

type referrerProxy struct {
	client      upstream.RegistryClient
	cache       backend.Backend
	ttl         time.Duration
	fallbackTag bool
}

func (p referrerProxy) Get(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error) {
	if err := ValidateRouteParts(req.UpstreamAlias, req.Repo); err != nil {
		return nil, err
	}
	cacheKey := referrersCacheKey(req)
	if p.cache != nil && p.ttl > 0 {
		data, ok, err := p.cache.Get(ctx, cacheKey)
		if err != nil {
			return nil, err
		}
		if ok {
			result, err := referrersFromEnvelope(data)
			if err != nil {
				_ = p.cache.Delete(ctx, cacheKey)
			} else {
				result.Cache = CacheHit
				return result, nil
			}
		}
	}

	result, err := p.fetch(ctx, req)
	if err != nil {
		return nil, err
	}
	if p.cache != nil && p.ttl > 0 {
		if data, err := referrersEnvelopeFromResult(result); err == nil {
			_ = p.cache.Set(ctx, cacheKey, data, p.ttl)
		}
	}
	return result, nil
}

func (p referrerProxy) fetch(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error) {
	resp, err := p.client.GetReferrers(ctx, upstream.ReferrersRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Digest:        req.Digest,
	})
	if err != nil {
		if p.fallbackTag && isManifestUnknown(err) {
			return p.fetchFallbackTag(ctx, req)
		}
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read referrers body: %w", err)
	}
	return &ReferrersResult{
		Body:      body,
		MediaType: resp.MediaType,
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

func (p referrerProxy) fetchFallbackTag(ctx context.Context, req ReferrerRequest) (*ReferrersResult, error) {
	reference, err := referrersFallbackReference(req.Digest)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.GetManifest(ctx, upstream.GetManifestRequest{
		UpstreamAlias: req.UpstreamAlias,
		Repo:          req.Repo,
		Reference:     reference,
		Accept:        distribution.MediaTypeOCIIndex,
		Method:        http.MethodGet,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read referrers fallback body: %w", err)
	}
	mediaType := resp.MediaType
	if mediaType == "" {
		mediaType = distribution.MediaTypeOCIIndex
	}
	return &ReferrersResult{
		Body:      body,
		MediaType: mediaType,
		Headers:   resp.Headers,
		Cache:     CacheBypass,
	}, nil
}

func isManifestUnknown(err error) bool {
	list := distribution.FromError(err)
	if list == nil {
		return false
	}
	for _, item := range list.Errors {
		if item.Code == distribution.CodeManifestUnknown {
			return true
		}
	}
	return false
}

func referrersFallbackReference(digest string) (string, error) {
	normalized, err := reference.NormalizeDigest(digest)
	if err != nil {
		return "", err
	}
	algorithm, encoded, _ := strings.Cut(normalized, ":")
	return algorithm + "-" + encoded, nil
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

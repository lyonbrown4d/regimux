package goproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

const (
	defaultMetadataTTL = 5 * time.Minute

	headerMirrorCache = "X-Mirror-Cache"
	cacheHit          = "hit"
	cacheMiss         = "miss"
	cacheStale        = "stale"
)

type ServiceDependencies struct {
	Config   config.Config
	Metadata meta.Store
	Objects  object.Store
	Logger   *slog.Logger
}

type Service struct {
	cfg      config.Config
	metadata meta.Store
	objects  object.Store
	client   *http.Client
	logger   *slog.Logger
}

type Request struct {
	Alias  string
	Tail   string
	Method string
}

type Response struct {
	Status      int
	Headers     http.Header
	Body        io.ReadCloser
	ContentType string
	Size        int64
	Cache       string
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

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:      deps.Config,
		metadata: deps.Metadata,
		objects:  deps.Objects,
		client:   &http.Client{},
		logger:   logger.With("component", "go-proxy"),
	}
}

func (s *Service) Get(ctx context.Context, req Request) (*Response, error) {
	if s == nil {
		return nil, oops.In("go-proxy").Errorf("service is nil")
	}
	route, err := parseRoute(req.Alias, req.Tail)
	if err != nil {
		return nil, err
	}
	upstreamCfg, ok := s.goUpstream(route.Alias)
	if !ok {
		return nil, oops.In("go-proxy").With("alias", route.Alias).Errorf("go upstream is not configured")
	}
	cached, cachedOK, err := s.cached(ctx, route)
	if err != nil {
		return nil, err
	}
	if cachedOK && !cached.expired {
		return s.responseFromStored(req, cached, cacheHit)
	}

	fetched, err := s.fetch(ctx, upstreamCfg, route, req.Method)
	if err != nil {
		if cachedOK {
			return s.responseFromStored(req, cached, cacheStale)
		}
		return nil, err
	}
	if fetched.status < http.StatusOK || fetched.status >= http.StatusMultipleChoices {
		return s.responseFromUpstream(req, fetched), nil
	}
	if methodOr(req.Method, http.MethodGet) == http.MethodHead {
		return s.responseFromUpstream(req, fetched), nil
	}
	if !routeCacheable(route) {
		return s.responseFromUpstream(req, fetched), nil
	}
	stored, err := s.store(ctx, route, fetched)
	if err != nil {
		return nil, err
	}
	return s.responseFromStored(req, storedResponse{
		digest:  stored.digest,
		size:    stored.size,
		headers: stored.headers,
		body:    stored.body,
	}, cacheMiss)
}

func (s *Service) goUpstream(alias string) (config.UpstreamConfig, bool) {
	if s.cfg.Upstreams == nil {
		return config.UpstreamConfig{}, false
	}
	cfg, ok := s.cfg.Upstreams[alias]
	if !ok || cfg.Type != "go" {
		return config.UpstreamConfig{}, false
	}
	return cfg, true
}

func (s *Service) cached(ctx context.Context, route route) (storedResponse, bool, error) {
	if s.metadata == nil || s.objects == nil {
		return storedResponse{}, false, nil
	}
	tag, ok, err := s.metadata.Tag(ctx, meta.TagKey{
		Alias:      route.Alias,
		Repository: route.Module,
		Reference:  route.Reference,
	})
	if err != nil {
		return storedResponse{}, false, wrapError(err, "lookup go proxy cache metadata")
	}
	if !ok {
		return storedResponse{}, false, nil
	}
	manifest, ok, err := s.metadata.Manifest(ctx, meta.ManifestKey{
		Alias:      route.Alias,
		Repository: route.Module,
		Digest:     tag.Digest,
	})
	if err != nil {
		return storedResponse{}, false, wrapError(err, "lookup go proxy content metadata")
	}
	if !ok {
		return storedResponse{}, false, nil
	}
	objectKey := manifest.ObjectKey
	if objectKey == "" {
		objectKey = manifest.Digest
	}
	reader, info, err := s.objects.Get(ctx, objectKey, object.GetOptions{})
	if errors.Is(err, object.ErrNotFound) {
		return storedResponse{}, false, nil
	}
	if err != nil {
		return storedResponse{}, false, wrapError(err, "open cached go proxy object")
	}
	size := manifest.Size
	if size <= 0 && info != nil {
		size = info.Size
	}
	return storedResponse{
		digest:  manifest.Digest,
		size:    size,
		headers: http.Header(manifest.Headers).Clone(),
		body:    reader,
		expired: expiredAt(tag.ExpiresAt, time.Now().UTC()) || manifest.Expired(time.Now().UTC()),
	}, true, nil
}

func (s *Service) fetch(ctx context.Context, cfg config.UpstreamConfig, route route, method string) (*upstreamFetch, error) {
	var lastErr error
	for _, endpoint := range upstreamEndpoints(cfg) {
		resp, err := s.fetchEndpoint(ctx, cfg, endpoint, route.Tail, method)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no go upstream endpoint is configured")
	}
	return nil, wrapError(lastErr, "fetch go proxy upstream")
}

func (s *Service) fetchEndpoint(ctx context.Context, cfg config.UpstreamConfig, endpoint, tail, method string) (*upstreamFetch, error) {
	requestURL := strings.TrimRight(endpoint, "/") + "/" + tail
	req, err := http.NewRequestWithContext(ctx, methodOr(method, http.MethodGet), requestURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "regimux/dev")
	applyAuth(req, cfg.Auth)

	client := s.client
	if cfg.HTTP.Timeout > 0 {
		client = &http.Client{Timeout: cfg.HTTP.Timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &upstreamFetch{
		status:  resp.StatusCode,
		headers: resp.Header.Clone(),
		body:    resp.Body,
	}, nil
}

func upstreamEndpoints(cfg config.UpstreamConfig) []string {
	out := make([]string, 0, 1+len(cfg.Mirrors))
	if cfg.Registry != "" {
		out = append(out, cfg.Registry)
	}
	out = append(out, cfg.Mirrors...)
	return out
}

func applyAuth(req *http.Request, cfg config.AuthConfig) {
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "basic":
		req.SetBasicAuth(cfg.Username, cfg.Password)
	case "bearer":
		if strings.TrimSpace(cfg.Token) != "" {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
		}
	}
}

func (s *Service) store(ctx context.Context, route route, fetched *upstreamFetch) (storedResponse, error) {
	if fetched == nil || fetched.body == nil {
		return storedResponse{}, oops.In("go-proxy").Errorf("go proxy upstream body is empty")
	}
	defer fetched.body.Close()

	tmp, err := os.CreateTemp("", "regimux-go-proxy-*")
	if err != nil {
		return storedResponse{}, wrapError(err, "create go proxy temp file")
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), fetched.body)
	if err != nil {
		tmp.Close()
		return storedResponse{}, wrapError(err, "write go proxy temp file")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return storedResponse{}, wrapError(err, "rewind go proxy temp file")
	}

	digest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	headers := cacheHeaders(fetched.headers, size)
	contentType := contentType(headers, route.Reference)
	info, err := s.objects.Put(ctx, digest, tmp, object.PutOptions{ContentType: contentType})
	closeErr := tmp.Close()
	if err != nil {
		return storedResponse{}, wrapError(err, "store go proxy object")
	}
	if closeErr != nil {
		return storedResponse{}, wrapError(closeErr, "close go proxy temp file")
	}

	if err := s.storeMetadata(ctx, route, digest, info, headers, contentType); err != nil {
		return storedResponse{}, err
	}
	reader, _, err := s.objects.Get(ctx, digest, object.GetOptions{})
	if err != nil {
		return storedResponse{}, wrapError(err, "open stored go proxy object")
	}
	return storedResponse{
		digest:  digest,
		size:    size,
		headers: headers,
		body:    reader,
	}, nil
}

func (s *Service) storeMetadata(ctx context.Context, route route, digest string, info *object.Info, headers http.Header, contentType string) error {
	if s.metadata == nil || info == nil {
		return nil
	}
	now := time.Now().UTC()
	expiresAt := time.Time{}
	if ttl := routeMetadataTTL(route, defaultMetadataTTL); ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	record := meta.ManifestRecord{
		Alias:      route.Alias,
		Repository: route.Module,
		Reference:  route.Reference,
		AcceptKey:  "go-proxy",
		Digest:     digest,
		MediaType:  contentType,
		Size:       info.Size,
		ObjectKey:  digest,
		Headers:    map[string][]string(headers.Clone()),
		ExpiresAt:  expiresAt,
	}
	if _, err := s.metadata.UpsertManifest(ctx, record); err != nil {
		return wrapError(err, "upsert go proxy content metadata")
	}
	if _, err := s.metadata.UpsertTag(ctx, meta.TagRecord{
		Alias:      route.Alias,
		Repository: route.Module,
		Reference:  route.Reference,
		Digest:     digest,
		ExpiresAt:  expiresAt,
	}); err != nil {
		return wrapError(err, "upsert go proxy request metadata")
	}
	if _, err := s.metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       digest,
		Size:         info.Size,
		MediaType:    contentType,
		ObjectKey:    digest,
		LastAccessAt: now,
	}); err != nil {
		return wrapError(err, "upsert go proxy blob metadata")
	}
	if _, err := s.metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          route.Alias,
		Repository:     route.Module,
		Digest:         digest,
		SourceManifest: digest,
		LastAccessAt:   now,
		LastVerifiedAt: now,
	}); err != nil {
		return wrapError(err, "upsert go proxy repository blob metadata")
	}
	return nil
}

func (s *Service) responseFromStored(req Request, stored storedResponse, cacheStatus string) (*Response, error) {
	headers := cacheHeaders(stored.headers, stored.size)
	headers.Set(headerMirrorCache, cacheStatus)
	if req.Method == http.MethodHead && stored.body != nil {
		if err := stored.body.Close(); err != nil {
			return nil, wrapError(err, "close cached go proxy object for head")
		}
		stored.body = http.NoBody
	}
	return &Response{
		Status:      http.StatusOK,
		Headers:     headers,
		Body:        stored.body,
		ContentType: contentType(headers, ""),
		Size:        stored.size,
		Cache:       cacheStatus,
	}, nil
}

func (s *Service) responseFromUpstream(req Request, fetched *upstreamFetch) *Response {
	headers := cacheHeaders(fetched.headers, -1)
	headers.Set(headerMirrorCache, cacheMiss)
	body := fetched.body
	if req.Method == http.MethodHead && body != nil {
		_ = body.Close()
		body = http.NoBody
	}
	size := contentLength(headers)
	return &Response{
		Status:      fetched.status,
		Headers:     headers,
		Body:        body,
		ContentType: contentType(headers, ""),
		Size:        size,
		Cache:       cacheMiss,
	}
}

func cacheHeaders(headers http.Header, size int64) http.Header {
	out := http.Header{}
	for _, key := range []string{
		"Cache-Control",
		"Content-Disposition",
		"Content-Encoding",
		"Content-Language",
		"Content-Type",
		"ETag",
		"Last-Modified",
	} {
		if values, ok := headers[key]; ok {
			out[key] = append([]string(nil), values...)
		}
	}
	if size >= 0 {
		out.Set(distribution.HeaderContentLength, strconv.FormatInt(size, 10))
	} else if value := headers.Get(distribution.HeaderContentLength); value != "" {
		out.Set(distribution.HeaderContentLength, value)
	}
	return out
}

func contentType(headers http.Header, reference string) string {
	if value := headers.Get(distribution.HeaderContentType); value != "" {
		return value
	}
	switch {
	case strings.HasSuffix(reference, ".zip"):
		return "application/zip"
	case strings.HasSuffix(reference, ".info"):
		return "application/json"
	default:
		return "text/plain; charset=utf-8"
	}
}

func contentLength(headers http.Header) int64 {
	value := headers.Get(distribution.HeaderContentLength)
	if value == "" {
		return -1
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1
	}
	return size
}

func methodOr(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func expiredAt(expiresAt, now time.Time) bool {
	return !expiresAt.IsZero() && !now.Before(expiresAt)
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("go-proxy").Wrapf(err, "%s", message)
}

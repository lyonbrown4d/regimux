package cache_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestManifestHeadMissDoesNotPoisonGetCache(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"schemaVersion":2}`)
	client := &fakeRegistryClient{
		manifestBody:  body,
		manifestMedia: distribution.MediaTypeDockerManifest,
	}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		backend.NewMemory(backend.MemoryOptions{}),
		config.Config{
			Cache: config.CacheConfig{
				Manifest: config.ManifestCacheConfig{
					TagTTL: time.Minute,
				},
			},
		},
	)

	head, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodHead,
	})
	if err != nil {
		t.Fatalf("head manifest get: %v", err)
	}
	if head.Cache != cache.CacheBypass || len(head.Body) != 0 {
		t.Fatalf("unexpected head result: cache=%s body=%q", head.Cache, head.Body)
	}

	get, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("get manifest after head: %v", err)
	}
	if get.Cache != cache.CacheBypass || !bytes.Equal(get.Body, body) {
		t.Fatalf("unexpected get result: cache=%s body=%q", get.Cache, get.Body)
	}
	if client.manifestGets != 2 {
		t.Fatalf("manifest gets = %d, want 2", client.manifestGets)
	}
}

func TestManifestProxyRecordsPullAndUpstreamPullTimes(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"schemaVersion":2}`)
	client := &fakeRegistryClient{
		manifestBody:  body,
		manifestMedia: distribution.MediaTypeOCIManifest,
	}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, nil, config.Config{})
	req := cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/node",
		Reference:     "20",
		Accept:        distribution.MediaTypeOCIManifest,
		Method:        http.MethodGet,
	}

	if _, err := proxy.Manifests().Get(ctx, req); err != nil {
		t.Fatalf("first manifest get: %v", err)
	}
	key := meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"}
	pull := requirePullRecord(ctx, t, metadata, key)
	assertPullRecordState(t, pull, 1, time.Time{})
	firstUpstreamPullAt := pull.LastUpstreamPullAt

	_, err := proxy.Manifests().Get(ctx, req)
	if err != nil {
		t.Fatalf("second manifest get: %v", err)
	}
	pull = requirePullRecord(ctx, t, metadata, key)
	assertPullRecordState(t, pull, 2, firstUpstreamPullAt)
}

func TestManifestProxyReturnsStaleOnUpstreamError(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"schemaVersion":2}`)
	client := &fakeRegistryClient{
		manifestBody:  body,
		manifestMedia: distribution.MediaTypeDockerManifest,
	}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		backend.NewMemory(backend.MemoryOptions{}),
		config.Config{
			Cache: config.CacheConfig{
				Manifest: config.ManifestCacheConfig{
					TagTTL:       time.Nanosecond,
					MaxStale:     time.Hour,
					StaleIfError: true,
				},
			},
		},
	)

	first, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first manifest get: %v", err)
	}
	if first.Cache != cache.CacheBypass {
		t.Fatalf("first cache status = %s, want bypass", first.Cache)
	}

	time.Sleep(5 * time.Millisecond)
	client.manifestErr = distribution.ErrUpstream.WithDetail("registry unavailable")
	stale, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("stale manifest get: %v", err)
	}
	if stale.Cache != cache.CacheStale || !bytes.Equal(stale.Body, body) {
		t.Fatalf("unexpected stale result: cache=%s body=%q", stale.Cache, stale.Body)
	}
	if got := stale.Headers.Get("Warning"); got != `110 - "Response is stale"` {
		t.Fatalf("warning = %q", got)
	}
}

func TestManifestProxyManifestCacheIgnoresAcceptMismatch(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"schemaVersion":2}`)
	client := &fakeRegistryClient{
		manifestBody:  body,
		manifestMedia: distribution.MediaTypeDockerManifest,
	}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		backend.NewMemory(backend.MemoryOptions{}),
		config.Config{
			Cache: config.CacheConfig{
				Manifest: config.ManifestCacheConfig{
					TagTTL: time.Minute,
				},
			},
		},
	)

	first, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Accept:        distribution.MediaTypeDockerManifest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first manifest get: %v", err)
	}
	if first.Cache != cache.CacheBypass {
		t.Fatalf("first cache status = %s, want bypass", first.Cache)
	}

	second, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Accept:        distribution.MediaTypeOCIManifest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second manifest get: %v", err)
	}
	if second.Cache != cache.CacheHit {
		t.Fatalf("second cache status = %s, want hit", second.Cache)
	}
	if !bytes.Equal(second.Body, body) {
		t.Fatalf("unexpected second body: %q", second.Body)
	}
	if client.manifestGets != 1 {
		t.Fatalf("manifest gets = %d, want 1", client.manifestGets)
	}
}

func TestManifestProxyServesExpiredTagStaleAndRevalidatesAsync(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"schemaVersion":2}`)
	client := &fakeRegistryClient{
		manifestBody:  body,
		manifestMedia: distribution.MediaTypeDockerManifest,
	}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		nil,
		config.Config{
			Cache: config.CacheConfig{
				Manifest: config.ManifestCacheConfig{
					TagTTL: 5 * time.Minute,
				},
			},
		},
	)

	first, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first manifest get: %v", err)
	}
	expireManifestMetadata(ctx, t, metadata, "hub", "library/alpine", "latest", first.Digest)
	second, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("stale manifest get: %v", err)
	}
	if second.Cache != cache.CacheStale || !bytes.Equal(second.Body, body) {
		t.Fatalf("unexpected stale result: cache=%s body=%q", second.Cache, second.Body)
	}
	third := waitForManifestCacheHit(t, ctx, proxy.Manifests(), cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodGet,
	})
	if third.Cache != cache.CacheHit || !bytes.Equal(third.Body, body) {
		t.Fatalf("unexpected post-refresh result: cache=%s body=%q", third.Cache, third.Body)
	}
	if client.manifestGets != 2 || client.manifestHeads != 1 {
		t.Fatalf("manifest calls = gets:%d heads:%d, want gets:2 heads:1", client.manifestGets, client.manifestHeads)
	}
	if first.Digest != third.Digest {
		t.Fatalf("digest changed: first=%s third=%s", first.Digest, third.Digest)
	}
}

func waitForManifestCacheHit(t *testing.T, ctx context.Context, service cache.ManifestService, req cache.ManifestRequest) *cache.CachedManifest {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var last *cache.CachedManifest
	for time.Now().Before(deadline) {
		got, err := service.Get(ctx, req)
		if err != nil {
			t.Fatalf("wait for manifest cache hit: %v", err)
		}
		last = got
		if got != nil && got.Cache == cache.CacheHit {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("manifest cache status = %s, want %s", last.Cache, cache.CacheHit)
	}
	t.Fatalf("manifest cache status was empty, want %s", cache.CacheHit)
	return nil
}

func expireManifestMetadata(ctx context.Context, t *testing.T, metadata meta.Store, alias, repo, reference, digest string) {
	t.Helper()
	expiresAt := time.Now().UTC().Add(-time.Minute)
	tag, ok, err := metadata.Tag(ctx, meta.TagKey{Alias: alias, Repository: repo, Reference: reference})
	if err != nil || !ok {
		t.Fatalf("lookup tag for expiration: ok=%v err=%v", ok, err)
	}
	tag.ExpiresAt = expiresAt
	if _, err := metadata.UpsertTag(ctx, *tag); err != nil {
		t.Fatalf("expire tag: %v", err)
	}
	manifest, ok, err := metadata.Manifest(ctx, meta.ManifestKey{Alias: alias, Repository: repo, Digest: digest})
	if err != nil || !ok {
		t.Fatalf("lookup manifest for expiration: ok=%v err=%v", ok, err)
	}
	manifest.ExpiresAt = expiresAt
	if _, err := metadata.UpsertManifest(ctx, *manifest); err != nil {
		t.Fatalf("expire manifest: %v", err)
	}
}

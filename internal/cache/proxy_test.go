package cache_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func newTestProxy(client upstream.RegistryClient, metadata meta.Store, objects object.Store, cacheBackend backend.Backend, cfg config.Config) *cache.Proxy {
	return cache.NewProxy(cache.ProxyDependencies{
		Client:   client,
		Cache:    cacheBackend,
		Metadata: metadata,
		Objects:  objects,
		Config:   cfg,
	})
}

func TestBlobProxyCachesMissAndServesRangeHit(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, nil, config.Config{})

	httpRange := &reference.HTTPRange{Start: 2, End: 5}
	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Range:         httpRange,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	assertRangeBlobMiss(t, first)

	second, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertFullBlobHit(t, second, body)
	if client.blobGets != 1 {
		t.Fatalf("expected one upstream blob GET, got %d", client.blobGets)
	}

	head, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodHead,
	})
	if err != nil {
		t.Fatalf("head blob get: %v", err)
	}
	assertHeadBlobHit(t, head, len(body))
}

func TestBlobProxyTouchesBlobAccessOnLocalHit(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, nil, config.Config{})

	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	_ = readAndClose(t, first.Reader)

	old := time.Now().UTC().Add(-2 * time.Hour)
	_, err = metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest:       digest,
		Size:         int64(len(body)),
		MediaType:    "application/octet-stream",
		ObjectKey:    digest,
		LastAccessAt: old,
	})
	if err != nil {
		t.Fatalf("set old blob access: %v", err)
	}
	_, err = metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias:          "hub",
		Repository:     "library/alpine",
		Digest:         digest,
		LastAccessAt:   old,
		LastVerifiedAt: old,
	})
	if err != nil {
		t.Fatalf("set old repo blob access: %v", err)
	}

	hit, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	assertFullBlobHit(t, hit, body)

	blob, ok, err := metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil || !ok {
		t.Fatalf("blob metadata lookup: ok=%v err=%v", ok, err)
	}
	if !blob.LastAccessAt.After(old) {
		t.Fatalf("blob access was not touched: old=%s got=%s", old, blob.LastAccessAt)
	}
	repoBlob, ok, err := metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      "hub",
		Repository: "library/alpine",
		Digest:     digest,
	})
	if err != nil || !ok {
		t.Fatalf("repo blob metadata lookup: ok=%v err=%v", ok, err)
	}
	if !repoBlob.LastAccessAt.After(old) {
		t.Fatalf("repo blob access was not touched: old=%s got=%s", old, repoBlob.LastAccessAt)
	}
	if !repoBlob.LastVerifiedAt.Equal(old) {
		t.Fatalf("repo blob verification time changed: old=%s got=%s", old, repoBlob.LastVerifiedAt)
	}
}

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
	pull, ok, err := metadata.Pull(ctx, meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"})
	if err != nil || !ok {
		t.Fatalf("pull lookup after miss: ok=%v err=%v", ok, err)
	}
	if pull.Count != 1 || pull.LastPullAt.IsZero() || pull.LastUpstreamPullAt.IsZero() {
		t.Fatalf("unexpected pull record after miss: %#v", pull)
	}
	firstUpstreamPullAt := pull.LastUpstreamPullAt

	if _, err := proxy.Manifests().Get(ctx, req); err != nil {
		t.Fatalf("second manifest get: %v", err)
	}
	pull, ok, err = metadata.Pull(ctx, meta.PullKey{Alias: "hub", Repository: "library/node", Reference: "20"})
	if err != nil || !ok {
		t.Fatalf("pull lookup after hit: ok=%v err=%v", ok, err)
	}
	if pull.Count != 2 || !pull.LastUpstreamPullAt.Equal(firstUpstreamPullAt) {
		t.Fatalf("unexpected pull record after hit: %#v", pull)
	}
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

func TestManifestProxyRevalidatesExpiredTagWithHead(t *testing.T) {
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
					TagTTL: time.Nanosecond,
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
	time.Sleep(5 * time.Millisecond)
	second, err := proxy.Manifests().Get(ctx, cache.ManifestRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Reference:     "latest",
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("revalidated manifest get: %v", err)
	}
	if second.Cache != cache.CacheHit || !bytes.Equal(second.Body, body) {
		t.Fatalf("unexpected revalidated result: cache=%s body=%q", second.Cache, second.Body)
	}
	if client.manifestGets != 2 || client.manifestHeads != 1 {
		t.Fatalf("manifest calls = gets:%d heads:%d, want gets:2 heads:1", client.manifestGets, client.manifestHeads)
	}
	if first.Digest != second.Digest {
		t.Fatalf("digest changed: first=%s second=%s", first.Digest, second.Digest)
	}
}

func TestTagProxyCachesAndRewritesLink(t *testing.T) {
	ctx := context.Background()
	client := &fakeRegistryClient{
		tagsBody: []byte(`{"name":"library/alpine","tags":["latest"]}`),
		tagsHeader: http.Header{
			"Link": {"<https://registry-1.docker.io/v2/library/alpine/tags/list?n=100&last=3.20>; rel=\"next\""},
		},
	}
	proxy := newTestProxy(
		client,
		nil,
		nil,
		backend.NewMemory(backend.MemoryOptions{}),
		config.Config{
			Cache: config.CacheConfig{
				Tags: config.TagsCacheConfig{
					TTL: time.Minute,
				},
			},
		},
	)

	first, err := proxy.Tags().List(ctx, cache.TagRequest{UpstreamAlias: "hub", Repo: "library/alpine", N: "100"})
	if err != nil {
		t.Fatalf("first tags list: %v", err)
	}
	if first.Cache != cache.CacheBypass {
		t.Fatalf("unexpected first cache status %s", first.Cache)
	}
	wantLink := "</v2/hub/library/alpine/tags/list?n=100&last=3.20>; rel=\"next\""
	if got := first.Headers.Get("Link"); got != wantLink {
		t.Fatalf("unexpected rewritten link %q", got)
	}

	second, err := proxy.Tags().List(ctx, cache.TagRequest{UpstreamAlias: "hub", Repo: "library/alpine", N: "100"})
	if err != nil {
		t.Fatalf("second tags list: %v", err)
	}
	if second.Cache != cache.CacheHit || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("unexpected second tags result: cache=%s body=%q", second.Cache, second.Body)
	}
	if client.tagsLists != 1 {
		t.Fatalf("expected one upstream tags request, got %d", client.tagsLists)
	}
}

func TestReferrersFallbackTagIsCached(t *testing.T) {
	ctx := context.Background()
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	client := &fakeRegistryClient{
		referrersErr:  distribution.ErrManifestUnknown.WithDetail("no referrers api"),
		manifestBody:  []byte(`{"schemaVersion":2,"manifests":[]}`),
		manifestMedia: distribution.MediaTypeOCIIndex,
	}
	proxy := newTestProxy(
		client,
		nil,
		nil,
		backend.NewMemory(backend.MemoryOptions{}),
		config.Config{
			Cache: config.CacheConfig{
				Referrers: config.ReferrersConfig{
					TTL:         time.Minute,
					FallbackTag: true,
				},
			},
		},
	)

	first, err := proxy.Referrers().Get(ctx, cache.ReferrerRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest})
	if err != nil {
		t.Fatalf("first referrers get: %v", err)
	}
	if first.Cache != cache.CacheBypass || first.MediaType != distribution.MediaTypeOCIIndex {
		t.Fatalf("unexpected first referrers result: cache=%s media=%s", first.Cache, first.MediaType)
	}
	if client.manifestReference != "sha256-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected fallback reference %q", client.manifestReference)
	}

	second, err := proxy.Referrers().Get(ctx, cache.ReferrerRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest})
	if err != nil {
		t.Fatalf("second referrers get: %v", err)
	}
	if second.Cache != cache.CacheHit || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("unexpected second referrers result: cache=%s body=%q", second.Cache, second.Body)
	}
	if client.referrersGets != 1 || client.manifestGets != 1 {
		t.Fatalf("unexpected upstream calls: referrers=%d manifests=%d", client.referrersGets, client.manifestGets)
	}
}

func TestBlobProxyStreamsRangeWhenEnabled(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		nil,
		config.Config{
			Cache: config.CacheConfig{
				Blob: config.BlobCacheConfig{
					StreamAndCache: true,
				},
			},
		},
	)

	httpRange := &reference.HTTPRange{Start: 2, End: 5}
	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Range:         httpRange,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	assertRangeBlobBypass(t, first)
	if client.blobGets != 1 {
		t.Fatalf("expected one upstream range GET, got %d", client.blobGets)
	}

	exists, err := objects.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("check object exists: %v", err)
	}
	if exists {
		t.Fatalf("blob should not be stored during range stream-and-cache path")
	}

	second, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	bodyBuf := readAndClose(t, second.Reader)
	if second.Cache != cache.CacheMiss || !bytes.Equal(bodyBuf, body) {
		t.Fatalf("unexpected second blob result: cache=%s body=%q", second.Cache, bodyBuf)
	}
	if client.blobGets != 2 {
		t.Fatalf("expected two upstream GETs, got %d", client.blobGets)
	}

	exists, err = objects.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("check object exists after full fetch: %v", err)
	}
	if !exists {
		t.Fatalf("blob should be stored after full fetch")
	}

	third, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Range:         httpRange,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("third blob get: %v", err)
	}
	assertRangeBlobHit(t, third)
	if client.blobGets != 2 {
		t.Fatalf("unexpected additional upstream GET after cached range: %d", client.blobGets)
	}
}

func TestBlobProxySkipsVerifyForRecentSharedBlobWithinTTL(t *testing.T) {
	ctx := context.Background()
	body := []byte("0123456789")
	digest := testDigestFor(body)
	client := &fakeRegistryClient{blobBody: body, blobDigest: digest}
	metadata, objects := newTestStores(t)
	verifyTTL := 5 * time.Minute
	proxy := newTestProxy(
		client,
		metadata,
		objects,
		nil,
		config.Config{
			Cache: config.CacheConfig{
				Blob: config.BlobCacheConfig{
					VerifyTTL: verifyTTL,
				},
			},
		},
	)

	first, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("first blob get: %v", err)
	}
	_ = readAndClose(t, first.Reader)
	if first.Cache != cache.CacheMiss {
		t.Fatalf("first cache status = %s, want miss", first.Cache)
	}
	if client.blobGets != 1 || client.blobHeads != 0 {
		t.Fatalf("unexpected first blob request counters: gets=%d heads=%d", client.blobGets, client.blobHeads)
	}

	record, ok, err := metadata.RepoBlob(ctx, meta.RepoBlobKey{
		Alias:      "hub",
		Repository: "library/alpine",
		Digest:     digest,
	})
	if err != nil || !ok {
		t.Fatalf("repo blob lookup: ok=%v err=%v", ok, err)
	}
	record.LastVerifiedAt = time.Now().UTC().Add(-verifyTTL / 2)
	if _, err := metadata.UpsertRepoBlob(ctx, *record); err != nil {
		t.Fatalf("refresh repo blob verify time: %v", err)
	}

	second, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("second blob get: %v", err)
	}
	_ = readAndClose(t, second.Reader)
	if second.Cache != cache.CacheHit {
		t.Fatalf("second cache status = %s, want hit", second.Cache)
	}
	if client.blobGets != 1 {
		t.Fatalf("unexpected upstream GET count after hit: %d", client.blobGets)
	}
	if client.blobHeads != 0 {
		t.Fatalf("expected verify skip within TTL, got head count %d", client.blobHeads)
	}

	record.LastVerifiedAt = time.Now().UTC().Add(-(verifyTTL + time.Second))
	if _, err := metadata.UpsertRepoBlob(ctx, *record); err != nil {
		t.Fatalf("expire repo blob verify time: %v", err)
	}

	third, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
		UpstreamAlias: "hub",
		Repo:          "library/alpine",
		Digest:        digest,
		Method:        http.MethodGet,
	})
	if err != nil {
		t.Fatalf("third blob get: %v", err)
	}
	_ = readAndClose(t, third.Reader)
	if third.Cache != cache.CacheHit {
		t.Fatalf("third cache status = %s, want hit", third.Cache)
	}
	if client.blobGets != 1 {
		t.Fatalf("unexpected upstream GET count after TTL retry: %d", client.blobGets)
	}
	if client.blobHeads != 1 {
		t.Fatalf("expected one upstream HEAD after TTL expiry, got %d", client.blobHeads)
	}
}

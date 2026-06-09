package cache_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestManifestProxyServesExpiredTagStaleUntilBackgroundRefresh(t *testing.T) {
	fixture := newManifestRefreshFixture(t)

	first := fixture.getManifest("first manifest get")
	fixture.expire(first.Digest)

	fixture.assertStaleHit("stale manifest get", 1, 0)
	fixture.refreshManifest()
	third := fixture.assertCacheHit("third manifest get", 2, 1)

	if first.Digest != third.Digest {
		t.Fatalf("digest changed: first=%s third=%s", first.Digest, third.Digest)
	}
}

type manifestRefreshFixture struct {
	t        *testing.T
	ctx      context.Context
	body     []byte
	client   *fakeRegistryClient
	metadata meta.Store
	proxy    *cache.Proxy
}

func newManifestRefreshFixture(t *testing.T) manifestRefreshFixture {
	t.Helper()
	body := []byte(`{"schemaVersion":2}`)
	client := &fakeRegistryClient{
		manifestBody:  body,
		manifestMedia: distribution.MediaTypeDockerManifest,
	}
	metadata, objects := newTestStores(t)
	proxy := newTestProxy(client, metadata, objects, nil, config.Config{
		Cache: config.CacheConfig{
			Manifest: config.ManifestCacheConfig{TagTTL: 5 * time.Minute},
		},
	})
	return manifestRefreshFixture{
		t:        t,
		ctx:      context.Background(),
		body:     body,
		client:   client,
		metadata: metadata,
		proxy:    proxy,
	}
}

func (f manifestRefreshFixture) getManifest(action string) *cache.CachedManifest {
	f.t.Helper()
	manifest, err := f.proxy.Manifests().Get(f.ctx, f.request(false))
	if err != nil {
		f.t.Fatalf("%s: %v", action, err)
	}
	return manifest
}

func (f manifestRefreshFixture) refreshManifest() {
	f.t.Helper()
	refresher, ok := f.proxy.Manifests().(cache.ManifestRefresher)
	if !ok {
		f.t.Fatal("manifest service does not implement refresher")
	}
	refreshed, err := refresher.Refresh(f.ctx, f.request(true))
	if err != nil {
		f.t.Fatalf("refresh manifest: %v", err)
	}
	f.requireManifest(refreshed, cache.CacheHit, "refreshed result")
}

func (f manifestRefreshFixture) assertStaleHit(action string, wantGets, wantHeads int) {
	f.t.Helper()
	manifest := f.getManifest(action)
	f.requireManifest(manifest, cache.CacheStale, "stale result")
	f.requireCalls("manifest calls after stale hit", wantGets, wantHeads)
}

func (f manifestRefreshFixture) assertCacheHit(action string, wantGets, wantHeads int) *cache.CachedManifest {
	f.t.Helper()
	manifest := f.getManifest(action)
	f.requireManifest(manifest, cache.CacheHit, "post-refresh result")
	f.requireCalls("manifest calls", wantGets, wantHeads)
	return manifest
}

func (f manifestRefreshFixture) requireManifest(manifest *cache.CachedManifest, wantCache cache.CacheStatus, label string) {
	f.t.Helper()
	if manifest.Cache != wantCache || !bytes.Equal(manifest.Body, f.body) {
		f.t.Fatalf("unexpected %s: cache=%s body=%q", label, manifest.Cache, manifest.Body)
	}
}

func (f manifestRefreshFixture) requireCalls(label string, wantGets, wantHeads int) {
	f.t.Helper()
	if f.client.manifestGets != wantGets || f.client.manifestHeads != wantHeads {
		f.t.Fatalf("%s = gets:%d heads:%d, want gets:%d heads:%d", label, f.client.manifestGets, f.client.manifestHeads, wantGets, wantHeads)
	}
}

func (f manifestRefreshFixture) request(refresh bool) cache.ManifestRequest {
	return cache.ManifestRequest{
		UpstreamAlias:  "hub",
		Repo:           "library/alpine",
		Reference:      "latest",
		Method:         http.MethodGet,
		SkipPullRecord: refresh,
	}
}

func (f manifestRefreshFixture) expire(digest string) {
	f.t.Helper()
	expiresAt := time.Now().UTC().Add(-time.Minute)
	tag, ok, err := f.metadata.Tag(f.ctx, meta.TagKey{Alias: "hub", Repository: "library/alpine", Reference: "latest"})
	if err != nil || !ok {
		f.t.Fatalf("lookup tag for expiration: ok=%v err=%v", ok, err)
	}
	tag.ExpiresAt = expiresAt
	if _, updateErr := f.metadata.UpsertTag(f.ctx, *tag); updateErr != nil {
		f.t.Fatalf("expire tag: %v", updateErr)
	}
	manifest, ok, err := f.metadata.Manifest(f.ctx, meta.ManifestKey{Alias: "hub", Repository: "library/alpine", Digest: digest})
	if err != nil || !ok {
		f.t.Fatalf("lookup manifest for expiration: ok=%v err=%v", ok, err)
	}
	manifest.ExpiresAt = expiresAt
	if _, updateErr := f.metadata.UpsertManifest(f.ctx, *manifest); updateErr != nil {
		f.t.Fatalf("expire manifest: %v", updateErr)
	}
}

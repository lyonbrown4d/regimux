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
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

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

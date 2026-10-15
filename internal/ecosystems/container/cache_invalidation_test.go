package container_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestInvalidateCacheCleanupDeletesOnlyRetiredManifestPrefixes(t *testing.T) {
	ctx := context.Background()
	store := backend.NewMemory(backend.MemoryOptions{Prefix: "acceptance"})
	proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store})
	runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
	accepts := []string{"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"}
	rootDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sharedChildDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	retiredRefs := []string{"1.36.1", rootDigest}
	retainedRefs := []string{"1.36.10", "stable", sharedChildDigest}
	for _, ref := range append(retiredRefs, retainedRefs...) {
		for _, accept := range accepts {
			key := testManifestKey("hub", "library/busybox", ref, accept)
			if err := store.Set(ctx, key, []byte("cached"), time.Minute); err != nil {
				t.Fatalf("seed cache key %q: %v", key, err)
			}
		}
	}
	if err := store.Set(ctx, "tags:hub:library/busybox::", []byte("tags"), time.Minute); err != nil {
		t.Fatalf("seed tags-list cache: %v", err)
	}

	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		Alias:     "hub",
		Package:   "library/busybox@" + rootDigest,
		Tags: []meta.TagRecord{{
			Alias: "hub", Repository: "library/busybox", Reference: "1.36.1", Digest: rootDigest,
		}},
		Manifests: []meta.ManifestRecord{{
			Alias: "hub", Repository: "library/busybox", Reference: "1.36.1", Digest: rootDigest,
		}},
	}
	if err := runtime.InvalidateCacheCleanup(ctx, group); err != nil {
		t.Fatalf("invalidate retired manifest cache entries: %v", err)
	}

	for _, ref := range retiredRefs {
		for _, accept := range accepts {
			key := testManifestKey("hub", "library/busybox", ref, accept)
			if _, found, err := store.Get(ctx, key); err != nil || found {
				t.Errorf("retired manifest key %q remains (found=%t, err=%v)", key, found, err)
			}
		}
	}
	for _, ref := range retainedRefs {
		for _, accept := range accepts {
			key := testManifestKey("hub", "library/busybox", ref, accept)
			if _, found, err := store.Get(ctx, key); err != nil || !found {
				t.Errorf("retained manifest key %q was removed (found=%t, err=%v)", key, found, err)
			}
		}
	}
	if _, found, err := store.Get(ctx, "tags:hub:library/busybox::"); err != nil || !found {
		t.Errorf("unrelated tags-list cache was removed (found=%t, err=%v)", found, err)
	}
}

func TestInvalidateCacheCleanupRequiresPrefixDeleteAndPropagatesErrors(t *testing.T) {
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		Tags: []meta.TagRecord{{
			Alias: "hub", Repository: "library/busybox", Reference: "latest",
		}},
	}
	t.Run("missing capability fails closed", func(t *testing.T) {
		store := backend.NewMemory(backend.MemoryOptions{})
		wrapped := backendOnlyStore{Backend: store}
		proxy := cache.NewProxy(cache.ProxyDependencies{Cache: wrapped})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		if err := runtime.InvalidateCacheCleanup(context.Background(), group); err == nil {
			t.Fatal("expected configured cache without prefix-delete capability to fail")
		}
	})

	t.Run("prefix delete error propagates", func(t *testing.T) {
		wantErr := errors.New("prefix delete failed")
		wrapped := failingPrefixStore{Backend: backend.NewMemory(backend.MemoryOptions{}), err: wantErr}
		proxy := cache.NewProxy(cache.ProxyDependencies{Cache: wrapped})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		if err := runtime.InvalidateCacheCleanup(context.Background(), group); !errors.Is(err, wantErr) {
			t.Fatalf("invalidation error = %v, want %v", err, wantErr)
		}
	})
}

func TestInvalidateCacheCleanupWithoutCacheOrReferencesIsNoop(t *testing.T) {
	t.Run("no cache", func(t *testing.T) {
		proxy := cache.NewProxy(cache.ProxyDependencies{})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		if err := runtime.InvalidateCacheCleanup(context.Background(), ecosystem.CacheCleanupGroup{}); err != nil {
			t.Fatalf("empty cleanup group with no cache: %v", err)
		}
	})
	t.Run("noop backend", func(t *testing.T) {
		proxy := cache.NewProxy(cache.ProxyDependencies{Cache: backend.Noop{}})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		group := ecosystem.CacheCleanupGroup{
			Ecosystem: ecosystem.Container,
			Tags: []meta.TagRecord{{
				Alias: "hub", Repository: "library/busybox", Reference: "latest",
			}},
		}
		if err := runtime.InvalidateCacheCleanup(context.Background(), group); err != nil {
			t.Fatalf("noop backend invalidation: %v", err)
		}
	})
}

func TestInvalidateCacheCleanupDeletesOnlyMaskedSmallBlobDigests(t *testing.T) {
	ctx := context.Background()
	retiredDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	adjacentDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab"
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		RepoBlobs: []meta.RepoBlobRecord{
			{Alias: "hub", Repository: "library/busybox", Digest: retiredDigest},
			{Alias: "hub", Repository: "library/alpine", Digest: retiredDigest}, // shared digest is deduplicated
		},
	}
	t.Run("enabled", func(t *testing.T) {
		store := backend.NewMemory(backend.MemoryOptions{})
		seedSmallBlobCache(t, ctx, store, retiredDigest, adjacentDigest)
		proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store, CacheConfig: smallBlobCacheEnabledConfig()})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		if err := runtime.InvalidateCacheCleanup(ctx, group); err != nil {
			t.Fatalf("invalidate small blob cache: %v", err)
		}
		assertCacheKey(t, ctx, store, "blob-small:"+retiredDigest, false)
		assertCacheKey(t, ctx, store, "blob-small:"+adjacentDigest, true)
	})
	t.Run("disabled", func(t *testing.T) {
		store := backend.NewMemory(backend.MemoryOptions{})
		seedSmallBlobCache(t, ctx, store, retiredDigest, adjacentDigest)
		proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		if err := runtime.InvalidateCacheCleanup(ctx, group); err != nil {
			t.Fatalf("invalidate with small cache disabled: %v", err)
		}
		assertCacheKey(t, ctx, store, "blob-small:"+retiredDigest, true)
		assertCacheKey(t, ctx, store, "blob-small:"+adjacentDigest, true)
	})
}

func TestInvalidateCacheCleanupPropagatesSmallBlobDeleteError(t *testing.T) {
	wantErr := errors.New("small blob delete failed")
	store := failingDeleteStore{Backend: backend.NewMemory(backend.MemoryOptions{}), err: wantErr}
	proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store, CacheConfig: smallBlobCacheEnabledConfig()})
	runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		RepoBlobs: []meta.RepoBlobRecord{{Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
	}
	if err := runtime.InvalidateCacheCleanup(context.Background(), group); !errors.Is(err, wantErr) {
		t.Fatalf("invalidation error = %v, want %v", err, wantErr)
	}
}

func TestInvalidateCacheCleanupDeletesOrphanSmallBlobDigest(t *testing.T) {
	ctx := context.Background()
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	store := backend.NewMemory(backend.MemoryOptions{})
	seedSmallBlobCache(t, ctx, store, digest)
	proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store, CacheConfig: smallBlobCacheEnabledConfig()})
	runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		Digests:   []string{digest},
	}
	if err := runtime.InvalidateCacheCleanup(ctx, group); err != nil {
		t.Fatalf("invalidate orphan small blob cache: %v", err)
	}
	assertCacheKey(t, ctx, store, "blob-small:"+digest, false)
}

func testManifestKey(alias, repository, ref, accept string) string {
	return fmt.Sprintf("manifest:%s:%s:%s:%s", alias, repository, ref, reference.AcceptKey(accept))
}

func smallBlobCacheEnabledConfig() config.CacheConfig {
	return config.CacheConfig{
		Blob: config.BlobCacheConfig{
			SmallCache: config.SmallBlobCacheConfig{Enabled: true},
		},
	}
}

func seedSmallBlobCache(t *testing.T, ctx context.Context, store backend.Backend, digests ...string) {
	t.Helper()
	for _, digest := range digests {
		if err := store.Set(ctx, "blob-small:"+digest, []byte("cached"), time.Minute); err != nil {
			t.Fatalf("seed small blob cache for %s: %v", digest, err)
		}
	}
}

func assertCacheKey(t *testing.T, ctx context.Context, store backend.Backend, key string, wantFound bool) {
	t.Helper()
	_, found, err := store.Get(ctx, key)
	if err != nil || found != wantFound {
		t.Errorf("cache key %q found=%t err=%v, want found=%t", key, found, err, wantFound)
	}
}

type backendOnlyStore struct {
	backend.Backend
}

type failingPrefixStore struct {
	backend.Backend
	err error
}

type failingDeleteStore struct {
	backend.Backend
	err error
}

func (s failingDeleteStore) Delete(context.Context, string) error {
	return s.err
}

func (s failingPrefixStore) DeletePrefix(context.Context, string) error {
	return s.err
}

var _ backend.PrefixDeleter = failingPrefixStore{}

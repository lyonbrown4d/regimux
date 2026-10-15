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
	retainedRefs := []string{"1.36.10", "stable", sharedChildDigest}
	retiredRefs := make([]string, 0, 2+len(retainedRefs))
	retiredRefs = append(retiredRefs, "1.36.1", rootDigest)
	seedManifestCacheReferences(ctx, t, store, append(retiredRefs, retainedRefs...), accepts)
	seedCacheEntry(ctx, t, store, "tags:hub:library/busybox::", []byte("tags"))

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

	assertManifestCacheReferences(ctx, t, store, retiredRefs, accepts, false)
	assertManifestCacheReferences(ctx, t, store, retainedRefs, accepts, true)
	assertCacheKey(ctx, t, store, "tags:hub:library/busybox::", true)
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
		seedSmallBlobCache(ctx, t, store, retiredDigest, adjacentDigest)
		proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store, CacheConfig: smallBlobCacheEnabledConfig()})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		if err := runtime.InvalidateCacheCleanup(ctx, group); err != nil {
			t.Fatalf("invalidate small blob cache: %v", err)
		}
		assertCacheKey(ctx, t, store, "blob-small:"+retiredDigest, false)
		assertCacheKey(ctx, t, store, "blob-small:"+adjacentDigest, true)
	})
	t.Run("disabled", func(t *testing.T) {
		store := backend.NewMemory(backend.MemoryOptions{})
		seedSmallBlobCache(ctx, t, store, retiredDigest, adjacentDigest)
		proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store})
		runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
		if err := runtime.InvalidateCacheCleanup(ctx, group); err != nil {
			t.Fatalf("invalidate with small cache disabled: %v", err)
		}
		assertCacheKey(ctx, t, store, "blob-small:"+retiredDigest, true)
		assertCacheKey(ctx, t, store, "blob-small:"+adjacentDigest, true)
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
	seedSmallBlobCache(ctx, t, store, digest)
	proxy := cache.NewProxy(cache.ProxyDependencies{Cache: store, CacheConfig: smallBlobCacheEnabledConfig()})
	runtime := container.NewRuntime(config.Config{}, nil, nil, proxy.Manifests(), nil)
	group := ecosystem.CacheCleanupGroup{
		Ecosystem: ecosystem.Container,
		Digests:   []string{digest},
	}
	if err := runtime.InvalidateCacheCleanup(ctx, group); err != nil {
		t.Fatalf("invalidate orphan small blob cache: %v", err)
	}
	assertCacheKey(ctx, t, store, "blob-small:"+digest, false)
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

func seedManifestCacheReferences(ctx context.Context, t *testing.T, store backend.Backend, refs, accepts []string) {
	t.Helper()
	for _, ref := range refs {
		for _, accept := range accepts {
			seedCacheEntry(ctx, t, store, testManifestKey("hub", "library/busybox", ref, accept), []byte("cached"))
		}
	}
}

func assertManifestCacheReferences(ctx context.Context, t *testing.T, store backend.Backend, refs, accepts []string, wantFound bool) {
	t.Helper()
	for _, ref := range refs {
		for _, accept := range accepts {
			assertCacheKey(ctx, t, store, testManifestKey("hub", "library/busybox", ref, accept), wantFound)
		}
	}
}

func seedSmallBlobCache(ctx context.Context, t *testing.T, store backend.Backend, digests ...string) {
	t.Helper()
	for _, digest := range digests {
		seedCacheEntry(ctx, t, store, "blob-small:"+digest, []byte("cached"))
	}
}

func seedCacheEntry(ctx context.Context, t *testing.T, store backend.Backend, key string, value []byte) {
	t.Helper()
	if err := store.Set(ctx, key, value, time.Minute); err != nil {
		t.Fatalf("seed cache key %q: %v", key, err)
	}
}

func assertCacheKey(ctx context.Context, t *testing.T, store backend.Backend, key string, wantFound bool) {
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

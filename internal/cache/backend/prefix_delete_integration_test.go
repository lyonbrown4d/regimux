package backend_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

type prefixCacheFactory struct {
	name string
	new  func(backend.KVOptions) (*backend.KV, error)
}

func TestPrefixDeletionAgainstRedisProtocol(t *testing.T) {
	address := os.Getenv("REGIMUX_TEST_PREFIX_REDIS_ADDR")
	if address == "" {
		t.Skip("REGIMUX_TEST_PREFIX_REDIS_ADDR is not set")
	}
	for _, factory := range []prefixCacheFactory{
		{name: "redis", new: backend.NewRedis},
		{name: "valkey", new: backend.NewValkey},
	} {
		t.Run(factory.name, func(t *testing.T) {
			verifyPrefixDeletionAgainstCache(t, address, factory)
		})
	}
}

func verifyPrefixDeletionAgainstCache(t *testing.T, address string, factory prefixCacheFactory) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("regimux-prefix-[literal]*?-%s-%d", factory.name, time.Now().UnixNano())
	cache, err := factory.new(backend.KVOptions{Addrs: []string{address}, Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"manifest:repo:v1:json", "manifest:repo:v1:oci", "manifest:repo:v10:json", "unrelated", "lease:check"}
	registerPrefixCacheCleanup(t, cache, keys)
	seedPrefixCacheKeys(ctx, t, cache, keys[:4])
	err = cache.DeletePrefix(ctx, "manifest:repo:v1:")
	if err != nil {
		t.Fatal(err)
	}
	assertPrefixCacheBoundary(ctx, t, cache, keys[:4])
	assertPrefixCacheLease(ctx, t, cache, keys[4])
}

func registerPrefixCacheCleanup(t *testing.T, cache *backend.KV, keys []string) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, key := range keys {
			if cleanupErr := cache.Delete(cleanupCtx, key); cleanupErr != nil {
				t.Error(cleanupErr)
			}
		}
		if closeErr := cache.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
}

func seedPrefixCacheKeys(ctx context.Context, t *testing.T, cache *backend.KV, keys []string) {
	t.Helper()
	for _, key := range keys {
		if err := cache.Set(ctx, key, []byte(key), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
}

func assertPrefixCacheBoundary(ctx context.Context, t *testing.T, cache *backend.KV, keys []string) {
	t.Helper()
	for index, key := range keys {
		_, found, err := cache.Get(ctx, key)
		if err != nil || found != (index >= 2) {
			t.Fatalf("key %s: found=%v err=%v", key, found, err)
		}
	}
}

func assertPrefixCacheLease(ctx context.Context, t *testing.T, cache *backend.KV, key string) {
	t.Helper()
	lease, acquired, err := cache.AcquireLease(ctx, key, time.Minute)
	if err != nil || !acquired || lease == nil {
		t.Fatalf("cache lease capability lost: acquired=%v lease=%v err=%v", acquired, lease, err)
	}
	if _, reacquired, reacquireErr := cache.AcquireLease(ctx, key, time.Minute); reacquireErr != nil || reacquired {
		t.Fatalf("held lease was reacquired: acquired=%v err=%v", reacquired, reacquireErr)
	}
	err = lease.Extend(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	err = lease.Release(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

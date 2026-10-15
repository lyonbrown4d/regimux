package backend_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

func TestPrefixDeletionAgainstRedisProtocol(t *testing.T) {
	address := os.Getenv("REGIMUX_TEST_PREFIX_REDIS_ADDR")
	if address == "" {
		t.Skip("REGIMUX_TEST_PREFIX_REDIS_ADDR is not set")
	}
	for name, factory := range map[string]func(backend.KVOptions) (*backend.KV, error){
		"redis": backend.NewRedis, "valkey": backend.NewValkey,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			prefix := fmt.Sprintf("regimux-prefix-[literal]*?-%s-%d", name, time.Now().UnixNano())
			cache, err := factory(backend.KVOptions{Addrs: []string{address}, Prefix: prefix})
			if err != nil {
				t.Fatal(err)
			}
			keys := []string{"manifest:repo:v1:json", "manifest:repo:v1:oci", "manifest:repo:v10:json", "unrelated", "lease:check"}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				for _, key := range keys {
					if err := cache.Delete(ctx, key); err != nil {
						t.Error(err)
					}
				}
				if err := cache.Close(); err != nil {
					t.Error(err)
				}
			})
			for _, key := range keys[:4] {
				if err := cache.Set(ctx, key, []byte(key), time.Minute); err != nil {
					t.Fatal(err)
				}
			}
			if err := cache.DeletePrefix(ctx, "manifest:repo:v1:"); err != nil {
				t.Fatal(err)
			}
			for i, key := range keys[:4] {
				_, found, err := cache.Get(ctx, key)
				if err != nil || found != (i >= 2) {
					t.Fatalf("key %s: found=%v err=%v", key, found, err)
				}
			}
			lease, acquired, err := cache.AcquireLease(ctx, "lease:check", time.Minute)
			if err != nil || !acquired || lease == nil {
				t.Fatalf("cache lease capability lost: acquired=%v lease=%v err=%v", acquired, lease, err)
			}
			if _, acquired, err := cache.AcquireLease(ctx, "lease:check", time.Minute); err != nil || acquired {
				t.Fatalf("held lease was reacquired: acquired=%v err=%v", acquired, err)
			}
			if err := lease.Extend(ctx, time.Minute); err != nil {
				t.Fatal(err)
			}
			if err := lease.Release(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

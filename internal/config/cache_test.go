// Package config_test verifies configuration loading through exported APIs.
package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadCacheDefaults(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}

	if cfg.Cache.Backend != "" {
		t.Fatalf("unexpected cache backend %q", cfg.Cache.Backend)
	}
	if cfg.Cache.Prefix != "regimux" {
		t.Fatalf("unexpected cache prefix %q", cfg.Cache.Prefix)
	}
	if cfg.Cache.DefaultTTL != 10*time.Minute {
		t.Fatalf("unexpected default ttl %s", cfg.Cache.DefaultTTL)
	}
	if cfg.Cache.Memory.MaxItems != 10000 {
		t.Fatalf("unexpected memory max items %d", cfg.Cache.Memory.MaxItems)
	}
	assertDefaultSmallBlobCache(t, cfg.Cache.Blob.SmallCache)
	if len(cfg.Cache.Redis.Addrs) == 0 {
		t.Fatal("expected redis default addrs")
	}
	if len(cfg.Cache.Valkey.Addrs) == 0 {
		t.Fatal("expected valkey default addrs")
	}
}

func assertDefaultSmallBlobCache(t *testing.T, small config.SmallBlobCacheConfig) {
	t.Helper()

	if small.Enabled {
		t.Fatalf("unexpected small blob cache default: %#v", small)
	}
	if small.MaxSizeBytes != 4*1024*1024 {
		t.Fatalf("unexpected small blob cache max size %d", small.MaxSizeBytes)
	}
	if small.TTL != 24*time.Hour {
		t.Fatalf("unexpected small blob cache ttl %s", small.TTL)
	}
}

func TestValidateCacheBackend(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}

	cfg.Cache.Backend = "unknown"
	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected unsupported cache backend error")
	}
}

func TestNormalizeSmallBlobCacheDefaults(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}

	cfg.Cache.Blob.SmallCache = config.SmallBlobCacheConfig{Enabled: true}
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	got := cfg.Cache.Blob.SmallCache
	if !got.Enabled || got.MaxSizeBytes != 4*1024*1024 || got.TTL != 24*time.Hour {
		t.Fatalf("unexpected small blob cache config: %#v", got)
	}
}

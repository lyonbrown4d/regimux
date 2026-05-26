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

	if cfg.Cache.Backend != "memory" {
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
	if len(cfg.Cache.Redis.Addrs) == 0 {
		t.Fatal("expected redis default addrs")
	}
	if len(cfg.Cache.Valkey.Addrs) == 0 {
		t.Fatal("expected valkey default addrs")
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

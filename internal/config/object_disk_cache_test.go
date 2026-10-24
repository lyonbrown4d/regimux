package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestDefaultObjectDiskCacheIsDisabledWithDocumentedLimits(t *testing.T) {
	cfg := loadDefaultConfig(t)
	diskCache := cfg.Store.Object.DiskCache
	if diskCache.Enabled {
		t.Fatal("object disk cache must default to disabled")
	}
	if diskCache.Directory != "./data/object-cache" || diskCache.MaxBytes != 10<<30 ||
		diskCache.MaxObjectBytes != 1<<30 || diskCache.MaxEntries != 10_000 ||
		diskCache.MaxConcurrentFills != 4 || diskCache.TTL != 24*time.Hour {
		t.Fatalf("unexpected object disk cache defaults: %#v", diskCache)
	}
	if cfg.Store.Object.Driver != "local" || cfg.Store.Object.Path != "data/objects" {
		t.Fatalf("disk cache defaults changed the default object store: %#v", cfg.Store.Object)
	}
}

func TestLoadObjectDiskCacheFromHCL(t *testing.T) {
	clearObjectDiskCacheEnv(t)
	path := filepath.Join(t.TempDir(), "regimux.hcl")
	if err := os.WriteFile(path, []byte(objectDiskCacheHCL(`
    enabled = true
    directory = "/var/cache/regimux/objects"
    max_bytes = 2097152
    max_object_bytes = 1048576
    max_entries = 21
    max_concurrent_fills = 3
    ttl = "6h"
`)), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load disk cache config: %v", err)
	}
	assertObjectDiskCacheConfig(t, cfg.Store.Object.DiskCache, true, "/var/cache/regimux/objects", 2<<20, 1<<20, 21, 3, 6*time.Hour)
}

func TestLoadObjectDiskCacheFromEnvironment(t *testing.T) {
	clearObjectDiskCacheEnv(t)
	path := filepath.Join(t.TempDir(), "regimux.hcl")
	if err := os.WriteFile(path, []byte(objectDiskCacheHCL("")), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("REGIMUX_STORE__OBJECT__DISK_CACHE__ENABLED", "true")
	t.Setenv("REGIMUX_STORE__OBJECT__DISK_CACHE__DIRECTORY", "/srv/regimux/object-cache")
	t.Setenv("REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_BYTES", "4194304")
	t.Setenv("REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_OBJECT_BYTES", "2097152")
	t.Setenv("REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_ENTRIES", "42")
	t.Setenv("REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_CONCURRENT_FILLS", "2")
	t.Setenv("REGIMUX_STORE__OBJECT__DISK_CACHE__TTL", "12h")

	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load environment disk cache config: %v", err)
	}
	assertObjectDiskCacheConfig(t, cfg.Store.Object.DiskCache, true, "/srv/regimux/object-cache", 4<<20, 2<<20, 42, 2, 12*time.Hour)
}

func TestValidateObjectDiskCacheConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*config.Config)
	}{
		{name: "requires s3 object driver", change: func(cfg *config.Config) { cfg.Store.Object.Driver = "local" }},
		{name: "rejects empty directory", change: func(cfg *config.Config) { cfg.Store.Object.DiskCache.Directory = " " }},
		{name: "rejects current directory", change: func(cfg *config.Config) { cfg.Store.Object.DiskCache.Directory = "." }},
		{name: "requires positive byte limit", change: func(cfg *config.Config) { cfg.Store.Object.DiskCache.MaxBytes = 0 }},
		{name: "requires positive object limit", change: func(cfg *config.Config) { cfg.Store.Object.DiskCache.MaxObjectBytes = 0 }},
		{name: "requires positive entry limit", change: func(cfg *config.Config) { cfg.Store.Object.DiskCache.MaxEntries = 0 }},
		{name: "requires positive fill limit", change: func(cfg *config.Config) { cfg.Store.Object.DiskCache.MaxConcurrentFills = 0 }},
		{name: "requires positive ttl", change: func(cfg *config.Config) { cfg.Store.Object.DiskCache.TTL = 0 }},
		{name: "object limit cannot exceed total limit", change: func(cfg *config.Config) {
			cfg.Store.Object.DiskCache.MaxObjectBytes = cfg.Store.Object.DiskCache.MaxBytes + 1
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validS3ObjectDiskCacheConfig()
			test.change(&cfg)
			if err := cfg.NormalizeAndValidate(); err == nil {
				t.Fatal("expected invalid object disk cache configuration to be rejected")
			}
		})
	}
	valid := validS3ObjectDiskCacheConfig()
	if err := valid.NormalizeAndValidate(); err != nil {
		t.Fatalf("valid object disk cache config rejected: %v", err)
	}
}

func objectDiskCacheHCL(diskCache string) string {
	return `
store {
  object {
    driver = "s3"
    s3 {
      bucket = "regimux-objects"
      region = "us-east-1"
    }
    disk_cache {` + diskCache + `
    }
  }
}
`
}

func clearObjectDiskCacheEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"REGIMUX_STORE__OBJECT__DISK_CACHE__ENABLED",
		"REGIMUX_STORE__OBJECT__DISK_CACHE__DIRECTORY",
		"REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_BYTES",
		"REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_OBJECT_BYTES",
		"REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_ENTRIES",
		"REGIMUX_STORE__OBJECT__DISK_CACHE__MAX_CONCURRENT_FILLS",
		"REGIMUX_STORE__OBJECT__DISK_CACHE__TTL",
	} {
		unsetEnv(t, key)
	}
}

func validS3ObjectDiskCacheConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Store.Object.Driver = "s3"
	cfg.Store.Object.Path = ""
	cfg.Store.Object.S3.Bucket = "regimux-objects"
	cfg.Store.Object.S3.Region = "us-east-1"
	cfg.Store.Object.DiskCache.Enabled = true
	return cfg
}

func assertObjectDiskCacheConfig(t *testing.T, got config.StoreObjectDiskCacheConfig, enabled bool, directory string,
	maxBytes, maxObjectBytes int64, maxEntries, maxConcurrentFills int, ttl time.Duration,
) {
	t.Helper()
	want := config.StoreObjectDiskCacheConfig{
		Enabled: enabled, Directory: directory, MaxBytes: maxBytes, MaxObjectBytes: maxObjectBytes,
		MaxEntries: maxEntries, MaxConcurrentFills: maxConcurrentFills, TTL: ttl,
	}
	if got != want {
		t.Fatalf("unexpected object disk cache config: got %#v, want %#v", got, want)
	}
}

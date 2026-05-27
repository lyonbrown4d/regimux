// Package config_test verifies configuration loading through exported APIs.
package config_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadDefaultsIncludeStore(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.Store.Meta.Driver != "bboltx" || cfg.Store.Meta.Path != "data/regimux.db" {
		t.Fatalf("unexpected meta store defaults: %#v", cfg.Store.Meta)
	}
	if cfg.Store.Object.Driver != "local" || cfg.Store.Object.Path != "data/objects" {
		t.Fatalf("unexpected object store defaults: %#v", cfg.Store.Object)
	}
}

func TestLoadDefaultsIncludeUpstreamBlobAndProbe(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	hub := cfg.Upstreams["hub"]
	if hub.Blob.MirrorPolicy != "ordered" || hub.Blob.TopN != 3 || hub.Blob.MaxConcurrencyPerEndpoint != 0 {
		t.Fatalf("unexpected upstream blob defaults: %#v", hub.Blob)
	}
	if cfg.Cache.Blob.VerifyTTL != 0 {
		t.Fatalf("unexpected blob verify ttl default: %s", cfg.Cache.Blob.VerifyTTL)
	}
	if hub.Probe.Enabled || hub.Probe.Interval != 30*time.Second || hub.Probe.Timeout != 3*time.Second || hub.Probe.Cooldown != 2*time.Minute {
		t.Fatalf("unexpected upstream probe defaults: %#v", hub.Probe)
	}
	if cfg.Worker.ProbeConcurrency != 16 || cfg.Worker.PrefetchConcurrency != 8 {
		t.Fatalf("unexpected worker defaults: %#v", cfg.Worker)
	}
	if cfg.Scheduler.Cleanup.MaxScan != 0 {
		t.Fatalf("unexpected cleanup max_scan default: %d", cfg.Scheduler.Cleanup.MaxScan)
	}
}

func TestValidateStoreRejectsUnsupportedDrivers(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	cfg.Store.Meta.Driver = "postgres"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected unsupported meta store driver error")
	}

	cfg, err = config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	cfg.Store.Object.Driver = "s3"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected unsupported object store driver error")
	}
}

func TestValidateStoreAcceptsMemoryObjectDriver(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	cfg.Store.Object.Driver = "MEMORY"
	cfg.Store.Object.Path = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr != nil {
		t.Fatalf("normalize memory object store: %v", normalizeErr)
	}
	if cfg.Store.Object.Driver != "memory" || cfg.Store.Object.Path != "data/objects" {
		t.Fatalf("unexpected object store config: %#v", cfg.Store.Object)
	}
}

func TestLoadHCLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regimux.hcl")
	if err := os.WriteFile(path, []byte(`
server {
  listen = "127.0.0.1:5555"
}

upstreams {
  local {
    registry = "https://example.com"
    mirrors = ["https://mirror-a.example.com", "https://mirror-b.example.com"]
    mirror_policy = "round_robin"

    blob {
      mirror_policy = "latency"
      top_n = 2
      max_concurrency_per_endpoint = 4
    }

    probe {
      enabled = true
      interval = "45s"
      timeout = "4s"
      cooldown = "90s"
    }
  }
}

worker {
  probe_concurrency = 5
  prefetch_concurrency = 7
}
`), 0o600); err != nil {
		t.Fatalf("write hcl config: %v", err)
	}

	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load hcl config: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:5555" {
		t.Fatalf("unexpected listen %q", cfg.Server.Listen)
	}
	if cfg.Upstreams["local"].Registry != "https://example.com" {
		t.Fatalf("unexpected upstream config: %#v", cfg.Upstreams["local"])
	}
	if got := cfg.Upstreams["local"].MirrorPolicy; got != "round_robin" {
		t.Fatalf("unexpected mirror policy %q", got)
	}
	if got := cfg.Upstreams["local"].Mirrors; len(got) != 2 || got[0] != "https://mirror-a.example.com" || got[1] != "https://mirror-b.example.com" {
		t.Fatalf("unexpected mirrors: %#v", got)
	}
	if got := cfg.Upstreams["local"].Blob; got.MirrorPolicy != "latency" || got.TopN != 2 || got.MaxConcurrencyPerEndpoint != 4 {
		t.Fatalf("unexpected blob config: %#v", got)
	}
	if got := cfg.Upstreams["local"].Probe; !got.Enabled || got.Interval != 45*time.Second || got.Timeout != 4*time.Second || got.Cooldown != 90*time.Second {
		t.Fatalf("unexpected probe config: %#v", got)
	}
	if got := cfg.Worker; got.ProbeConcurrency != 5 || got.PrefetchConcurrency != 7 {
		t.Fatalf("unexpected worker config: %#v", got)
	}
}

func TestNormalizeUpstreamBlobDefaultsToMirrorPolicy(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	local := cfg.Upstreams["hub"]
	local.MirrorPolicy = "round_robin"
	local.Blob = config.UpstreamBlobConfig{}
	cfg.Upstreams = map[string]config.UpstreamConfig{"local": local}

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize upstream: %v", err)
	}
	if got := cfg.Upstreams["local"].Blob; got.MirrorPolicy != "round_robin" || got.TopN != 3 {
		t.Fatalf("unexpected blob defaults: %#v", got)
	}
}

func TestNormalizeLatencyBlobPolicyEnablesProbe(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	local := cfg.Upstreams["hub"]
	local.Blob.MirrorPolicy = "latency"
	local.Probe.Enabled = false
	cfg.Upstreams = map[string]config.UpstreamConfig{"local": local}

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize upstream: %v", err)
	}
	if got := cfg.Upstreams["local"].Probe; !got.Enabled {
		t.Fatalf("latency blob policy did not enable probe: %#v", got)
	}
}

func TestValidateUpstreamBlobAndProbeRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{
			name: "blob policy",
			mutate: func(cfg *config.Config) {
				upstreamCfg := cfg.Upstreams["hub"]
				upstreamCfg.Blob.MirrorPolicy = "fastest"
				cfg.Upstreams["hub"] = upstreamCfg
			},
		},
		{
			name: "blob top n",
			mutate: func(cfg *config.Config) {
				upstreamCfg := cfg.Upstreams["hub"]
				upstreamCfg.Blob.TopN = -1
				cfg.Upstreams["hub"] = upstreamCfg
			},
		},
		{
			name: "blob max concurrency",
			mutate: func(cfg *config.Config) {
				upstreamCfg := cfg.Upstreams["hub"]
				upstreamCfg.Blob.MaxConcurrencyPerEndpoint = -1
				cfg.Upstreams["hub"] = upstreamCfg
			},
		},
		{
			name: "probe interval",
			mutate: func(cfg *config.Config) {
				upstreamCfg := cfg.Upstreams["hub"]
				upstreamCfg.Probe.Interval = -time.Second
				cfg.Upstreams["hub"] = upstreamCfg
			},
		},
		{
			name: "probe timeout",
			mutate: func(cfg *config.Config) {
				upstreamCfg := cfg.Upstreams["hub"]
				upstreamCfg.Probe.Timeout = -time.Second
				cfg.Upstreams["hub"] = upstreamCfg
			},
		},
		{
			name: "probe cooldown",
			mutate: func(cfg *config.Config) {
				upstreamCfg := cfg.Upstreams["hub"]
				upstreamCfg.Probe.Cooldown = -time.Second
				cfg.Upstreams["hub"] = upstreamCfg
			},
		},
		{
			name: "worker probe concurrency",
			mutate: func(cfg *config.Config) {
				cfg.Worker.ProbeConcurrency = -1
			},
		},
		{
			name: "worker prefetch concurrency",
			mutate: func(cfg *config.Config) {
				cfg.Worker.PrefetchConcurrency = -1
			},
		},
		{
			name: "cleanup max_scan",
			mutate: func(cfg *config.Config) {
				cfg.Scheduler.Cleanup.MaxScan = -1
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(context.Background(), "")
			if err != nil {
				t.Fatalf("load defaults: %v", err)
			}
			tt.mutate(&cfg)
			if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
				t.Fatal("expected upstream blob/probe validation error")
			}
		})
	}
}

func TestLoadRejectsNonHCLFile(t *testing.T) {
	if _, err := config.Load(context.Background(), "regimux.yaml"); err == nil {
		t.Fatal("expected non-HCL config file error")
	}
}

func TestLoadEnvAndDotenv(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, "REGIMUX_SERVER__LISTEN")
	if err := os.WriteFile(".env", []byte("REGIMUX_SERVER__LISTEN=127.0.0.1:7777\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load dotenv config: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:7777" {
		t.Fatalf("unexpected dotenv listen %q", cfg.Server.Listen)
	}

	t.Setenv("REGIMUX_SERVER__LISTEN", "127.0.0.1:8888")
	cfg, err = config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load env config: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:8888" {
		t.Fatalf("unexpected env listen %q", cfg.Server.Listen)
	}
}

func TestLoadCommandLineOverrides(t *testing.T) {
	cfg, err := config.Load(context.Background(), "", "--server.listen=:7777", "--worker.probe_concurrency=7")
	if err != nil {
		t.Fatalf("load with command-line overrides: %v", err)
	}
	if cfg.Server.Listen != ":7777" {
		t.Fatalf("unexpected server.listen %q", cfg.Server.Listen)
	}
	if cfg.Worker.ProbeConcurrency != 7 {
		t.Fatalf("unexpected worker.probe_concurrency %d", cfg.Worker.ProbeConcurrency)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()

	original, hadOriginal := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if err := restoreEnv(key, original, hadOriginal); err != nil {
			t.Errorf("restore %s: %v", key, err)
		}
	})
}

func restoreEnv(key, value string, shouldSet bool) error {
	if shouldSet {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %s: %w", key, err)
		}
		return nil
	}
	if err := os.Unsetenv(key); err != nil {
		return fmt.Errorf("unset env %s: %w", key, err)
	}
	return nil
}

// Package config_test verifies configuration loading through exported APIs.
package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadDefaultsIncludeStore(t *testing.T) {
	cfg := loadDefaultConfig(t)
	if cfg.Store.Meta.Driver != "sqlite" || cfg.Store.Meta.Path != "data/regimux.db" {
		t.Fatalf("unexpected meta store defaults: %#v", cfg.Store.Meta)
	}
	if cfg.Store.Object.Driver != "local" || cfg.Store.Object.Path != "data/objects" {
		t.Fatalf("unexpected object store defaults: %#v", cfg.Store.Object)
	}
}

func TestLoadDefaultsDisableRegistryAuth(t *testing.T) {
	cfg := loadDefaultConfig(t)
	if cfg.Auth.Enabled {
		t.Fatal("expected registry auth to be disabled by default")
	}
	if cfg.Auth.Service != "regimux" || cfg.Auth.Issuer != "regimux" || cfg.Auth.TokenTTL != 15*time.Minute {
		t.Fatalf("unexpected auth defaults: %#v", cfg.Auth)
	}
}

func TestLoadDefaultsIncludeUpstreamBlobAndProbe(t *testing.T) {
	cfg := loadDefaultConfig(t)
	hub := cfg.Upstreams["hub"]
	assertDefaultUpstreamBlob(t, hub.Blob)
	assertDefaultUpstreamProbe(t, hub.Probe)
	assertDefaultWorker(t, cfg.Worker)
	assertDefaultCleanup(t, cfg.Scheduler.Cleanup)
	if cfg.Cache.Blob.VerifyTTL != 0 {
		t.Fatalf("unexpected blob verify ttl default: %s", cfg.Cache.Blob.VerifyTTL)
	}
}

func loadDefaultConfig(t *testing.T) config.Config {
	t.Helper()

	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	return cfg
}

func assertDefaultUpstreamBlob(t *testing.T, blob config.UpstreamBlobConfig) {
	t.Helper()

	if blob.MirrorPolicy != "ordered" || blob.TopN != 3 || blob.MaxConcurrencyPerEndpoint != 0 {
		t.Fatalf("unexpected upstream blob defaults: %#v", blob)
	}
	if blob.MaxConcurrentAttempts != 1 {
		t.Fatalf("unexpected upstream blob concurrent attempts default: %d", blob.MaxConcurrentAttempts)
	}
}

func assertDefaultUpstreamProbe(t *testing.T, probe config.UpstreamProbeConfig) {
	t.Helper()

	if probe.Enabled || probe.Interval != 30*time.Second || probe.Timeout != 3*time.Second || probe.Cooldown != 2*time.Minute {
		t.Fatalf("unexpected upstream probe defaults: %#v", probe)
	}
}

func assertDefaultWorker(t *testing.T, worker config.WorkerConfig) {
	t.Helper()

	if worker.ProbeConcurrency != 16 || worker.PrefetchConcurrency != 8 {
		t.Fatalf("unexpected worker defaults: %#v", worker)
	}
}

func assertDefaultCleanup(t *testing.T, cleanup config.SchedulerCleanupConfig) {
	t.Helper()

	if cleanup.MaxScan != 0 {
		t.Fatalf("unexpected cleanup max_scan default: %d", cleanup.MaxScan)
	}
}

func TestLoadHCLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regimux.hcl")
	if err := os.WriteFile(path, []byte(`
server {
  listen = "127.0.0.1:5555"
}

auth {
  enabled = true
  service = "regimux"
  issuer = "regimux"
  token_secret = "test-secret"
  token_ttl = "10m"

  users {
    alice {
      password = "secret"
      repositories = ["local/*"]
      groups = ["developers"]
    }
  }
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
      max_concurrent_attempts = 3
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
	assertLoadedHCLConfig(t, cfg)
}

func assertLoadedHCLConfig(t *testing.T, cfg config.Config) {
	t.Helper()

	if cfg.Server.Listen != "127.0.0.1:5555" {
		t.Fatalf("unexpected listen %q", cfg.Server.Listen)
	}
	assertLoadedHCLAuth(t, cfg.Auth)
	assertLoadedHCLUpstream(t, cfg.Upstreams["local"])
	assertLoadedHCLWorker(t, cfg.Worker)
}

func assertLoadedHCLAuth(t *testing.T, auth config.RegistryAuthConfig) {
	t.Helper()

	if !auth.Enabled || auth.Service != "regimux" || auth.Issuer != "regimux" || auth.TokenTTL != 10*time.Minute {
		t.Fatalf("unexpected auth config: %#v", auth)
	}
	user, ok := auth.Users["alice"]
	if !ok {
		t.Fatalf("missing auth user alice: %#v", auth.Users)
	}
	if user.Password != "secret" || len(user.Repositories) != 1 || user.Repositories[0] != "local/*" {
		t.Fatalf("unexpected auth user: %#v", user)
	}
}

func assertLoadedHCLUpstream(t *testing.T, upstreamCfg config.UpstreamConfig) {
	t.Helper()

	if upstreamCfg.Registry != "https://example.com" {
		t.Fatalf("unexpected upstream config: %#v", upstreamCfg)
	}
	if got := upstreamCfg.MirrorPolicy; got != "round_robin" {
		t.Fatalf("unexpected mirror policy %q", got)
	}
	assertLoadedHCLMirrors(t, upstreamCfg.Mirrors)
	assertLoadedHCLBlob(t, upstreamCfg.Blob)
	assertLoadedHCLProbe(t, upstreamCfg.Probe)
}

func assertLoadedHCLMirrors(t *testing.T, mirrors []string) {
	t.Helper()

	if len(mirrors) != 2 || mirrors[0] != "https://mirror-a.example.com" || mirrors[1] != "https://mirror-b.example.com" {
		t.Fatalf("unexpected mirrors: %#v", mirrors)
	}
}

func assertLoadedHCLBlob(t *testing.T, blob config.UpstreamBlobConfig) {
	t.Helper()

	if blob.MirrorPolicy != "latency" || blob.TopN != 2 || blob.MaxConcurrencyPerEndpoint != 4 || blob.MaxConcurrentAttempts != 3 {
		t.Fatalf("unexpected blob config: %#v", blob)
	}
}

func assertLoadedHCLProbe(t *testing.T, probe config.UpstreamProbeConfig) {
	t.Helper()

	if !probe.Enabled || probe.Interval != 45*time.Second || probe.Timeout != 4*time.Second || probe.Cooldown != 90*time.Second {
		t.Fatalf("unexpected probe config: %#v", probe)
	}
}

func assertLoadedHCLWorker(t *testing.T, worker config.WorkerConfig) {
	t.Helper()

	if got := worker; got.ProbeConcurrency != 5 || got.PrefetchConcurrency != 7 {
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
	if got := cfg.Upstreams["local"].Blob; got.MirrorPolicy != "round_robin" || got.TopN != 3 || got.MaxConcurrentAttempts != 1 {
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

func TestLoadRejectsNonHCLFile(t *testing.T) {
	if _, err := config.Load(context.Background(), "regimux.yaml"); err == nil {
		t.Fatal("expected non-HCL config file error")
	}
}

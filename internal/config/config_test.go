// Package config_test verifies configuration loading through exported APIs.
package config_test

import (
	"context"
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
	if hub.Type != "oci" {
		t.Fatalf("unexpected hub upstream type: %q", hub.Type)
	}
	golang := cfg.Upstreams["golang"]
	if golang.Type != "go" || golang.Registry != "https://proxy.golang.org" {
		t.Fatalf("unexpected golang upstream defaults: %#v", golang)
	}
	assertDefaultUpstreamBlob(t, hub.Blob)
	assertDefaultUpstreamProbe(t, hub.Probe)
	assertDefaultWorker(t, cfg.Worker)
	assertDefaultCleanup(t, cfg.Scheduler.Cleanup)
	assertDefaultPrefetch(t, cfg.Scheduler.Prefetch)
	if cfg.Cache.Blob.VerifyTTL != 0 {
		t.Fatalf("unexpected blob verify ttl default: %s", cfg.Cache.Blob.VerifyTTL)
	}
	if !cfg.Cache.Blob.StreamAndCache {
		t.Fatal("expected stream-and-cache to be enabled by default")
	}
	if hub.HTTP.HTTP2.Enabled {
		t.Fatalf("unexpected upstream http2 default: %#v", hub.HTTP.HTTP2)
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

	if probe.Enabled || probe.Interval != 30*time.Second || probe.Timeout != 3*time.Second ||
		probe.Cooldown != 2*time.Minute || probe.Jitter != 5*time.Second {
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

func assertDefaultPrefetch(t *testing.T, prefetch config.SchedulerPrefetchConfig) {
	t.Helper()

	if prefetch.MaxBytes != 0 || prefetch.MaxTasks != 0 || prefetch.MaxRepositories != 0 {
		t.Fatalf("unexpected prefetch policy limits: %#v", prefetch)
	}
	if prefetch.FailureBackoff != time.Hour || prefetch.RetryWindow != 24*time.Hour {
		t.Fatalf("unexpected prefetch retry defaults: %#v", prefetch)
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

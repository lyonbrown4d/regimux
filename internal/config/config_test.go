// Package config_test verifies configuration loading through exported APIs.
package config_test

import (
	"context"
	"runtime"
	"slices"
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
	hub := assertDefaultContainerUpstreams(t, cfg)
	assertDefaultDependencyUpstreams(t, cfg)
	assertDefaultContainerPrewarm(t, cfg.Container["hub"].Prewarm)
	assertDefaultContainerPrewarm(t, hub.Prewarm)
	assertDefaultUpstreamBlob(t, hub.Blob)
	assertDefaultUpstreamProbe(t, hub.Probe)
	assertDefaultWorker(t, cfg.Worker)
	assertDefaultCleanup(t, cfg.Scheduler.Cleanup)
	assertDefaultPrefetch(t, cfg.Scheduler.Prefetch)
	assertDefaultRefresh(t, cfg.Scheduler.Refresh)
	assertDefaultBlobCache(t, cfg.Cache.Blob)
	assertDefaultManifestRefresh(t, cfg.Scheduler.ManifestRefresh)
	if hub.HTTP.HTTP2.Enabled {
		t.Fatalf("unexpected upstream http2 default: %#v", hub.HTTP.HTTP2)
	}
}

func assertDefaultContainerUpstreams(t *testing.T, cfg config.Config) config.UpstreamConfig {
	t.Helper()

	hub, ok := cfg.ContainerUpstream("hub")
	if !ok {
		t.Fatal("missing hub container upstream")
	}
	if hub.Type != "oci" {
		t.Fatalf("unexpected hub upstream type: %q", hub.Type)
	}
	if cfg.Container["hub"].Registry != "https://registry-1.docker.io" ||
		cfg.Container["ghcr"].Registry != "https://ghcr.io" ||
		cfg.Container["quay"].Registry != "https://quay.io" {
		t.Fatalf("unexpected container defaults: %#v", cfg.Container)
	}
	return hub
}

func assertDefaultContainerPrewarm(t *testing.T, prewarm config.ContainerPrewarmConfig) {
	t.Helper()

	if !slices.Equal(prewarm.Platforms, []string{config.DefaultContainerPrewarmPlatform()}) {
		t.Fatalf("unexpected container prewarm defaults: %#v", prewarm)
	}
}

func assertDefaultDependencyUpstreams(t *testing.T, cfg config.Config) {
	t.Helper()

	golang, ok := cfg.GoUpstream("default")
	if !ok {
		t.Fatal("missing default go upstream")
	}
	if golang.Type != "go" || golang.Registry != "https://proxy.golang.org" {
		t.Fatalf("unexpected golang upstream defaults: %#v", golang)
	}
	if cfg.Go["default"].Registry != "https://proxy.golang.org" {
		t.Fatalf("unexpected go defaults: %#v", cfg.Go)
	}
	if cfg.Maven["central"].Registry != "https://repo.maven.apache.org/maven2" {
		t.Fatalf("unexpected maven defaults: %#v", cfg.Maven)
	}
	if cfg.PyPI["default"].Registry != "https://pypi.org" {
		t.Fatalf("unexpected pypi defaults: %#v", cfg.PyPI)
	}
	if cfg.NPM["default"].Registry != "https://registry.npmjs.org" {
		t.Fatalf("unexpected npm defaults: %#v", cfg.NPM)
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

	if worker.IOConcurrency != runtime.NumCPU()*2+1 || worker.LeaseConcurrency != 64 {
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

func assertDefaultRefresh(t *testing.T, refresh config.SchedulerRefreshConfig) {
	t.Helper()

	if !refresh.Enabled || refresh.Window != 10*time.Minute || !refresh.Distributed {
		t.Fatalf("unexpected refresh defaults: %#v", refresh)
	}
}

func assertDefaultManifestRefresh(t *testing.T, manifestRefresh config.SchedulerManifestRefreshConfig) {
	t.Helper()

	if manifestRefresh.Enabled || manifestRefresh.Interval != 30*time.Minute || !manifestRefresh.Distributed ||
		len(manifestRefresh.Ecosystems) != 0 {
		t.Fatalf("unexpected manifest_refresh defaults: %#v", manifestRefresh)
	}
}

func assertDefaultBlobCache(t *testing.T, blob config.BlobCacheConfig) {
	t.Helper()

	if blob.VerifyTTL != 0 {
		t.Fatalf("unexpected blob verify ttl default: %s", blob.VerifyTTL)
	}
	if !blob.StreamAndCache {
		t.Fatal("expected stream-and-cache to be enabled by default")
	}
}

func TestNormalizeUpstreamBlobDefaultsToMirrorPolicy(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	local := cfg.Container["hub"]
	local.MirrorPolicy = "round_robin"
	local.Blob = config.UpstreamBlobConfig{}
	cfg.Container = config.ContainerConfig{"local": local}

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize upstream: %v", err)
	}
	localUpstream, ok := cfg.ContainerUpstream("local")
	if !ok {
		t.Fatal("missing local container upstream")
	}
	if got := localUpstream.Blob; got.MirrorPolicy != "round_robin" || got.TopN != 3 || got.MaxConcurrentAttempts != 1 {
		t.Fatalf("unexpected blob defaults: %#v", got)
	}
}

func TestNormalizeLatencyBlobPolicyEnablesProbe(t *testing.T) {
	cfg, err := config.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	local := cfg.Container["hub"]
	local.Blob.MirrorPolicy = "latency"
	local.Probe.Enabled = false
	cfg.Container = config.ContainerConfig{"local": local}

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("normalize upstream: %v", err)
	}
	localUpstream, ok := cfg.ContainerUpstream("local")
	if !ok {
		t.Fatal("missing local container upstream")
	}
	if got := localUpstream.Probe; !got.Enabled {
		t.Fatalf("latency blob policy did not enable probe: %#v", got)
	}
}

func TestLoadRejectsNonHCLFile(t *testing.T) {
	if _, err := config.Load(context.Background(), "regimux.yaml"); err == nil {
		t.Fatal("expected non-HCL config file error")
	}
}

package config_test

import (
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

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

func assertLoadedHCLCache(t *testing.T, cache config.CacheConfig) {
	t.Helper()

	small := cache.Blob.SmallCache
	if !small.Enabled || small.MaxSizeBytes != 1024 || small.TTL != 2*time.Hour {
		t.Fatalf("unexpected small blob cache config: %#v", small)
	}
}

func assertLoadedHCLUpstream(t *testing.T, upstreamCfg config.UpstreamConfig) {
	t.Helper()

	if upstreamCfg.Registry != "https://example.com" {
		t.Fatalf("unexpected upstream config: %#v", upstreamCfg)
	}
	if upstreamCfg.Type != "oci" {
		t.Fatalf("unexpected upstream type %q", upstreamCfg.Type)
	}
	if got := upstreamCfg.MirrorPolicy; got != "round_robin" {
		t.Fatalf("unexpected mirror policy %q", got)
	}
	assertLoadedHCLMirrors(t, upstreamCfg.Mirrors)
	assertLoadedHCLBlob(t, upstreamCfg.Blob)
	assertLoadedHCLProbe(t, upstreamCfg.Probe)
	if !upstreamCfg.HTTP.HTTP2.Enabled {
		t.Fatalf("unexpected upstream http2 config: %#v", upstreamCfg.HTTP.HTTP2)
	}
}

func assertLoadedHCLEcosystemConfig(t *testing.T, cfg config.Config) {
	t.Helper()

	if cfg.Container["local"].Registry != "https://example.com" {
		t.Fatalf("unexpected container ecosystem config: %#v", cfg.Container["local"])
	}
	assertLoadedHCLGoConfig(t, cfg)
	assertLoadedHCLNPMConfig(t, cfg)
	assertLoadedHCLPyPIConfig(t, cfg)
	assertLoadedHCLMavenConfig(t, cfg)
}

func assertLoadedHCLGoConfig(t *testing.T, cfg config.Config) {
	t.Helper()

	goUpstream, ok := cfg.GoUpstream("default")
	if !ok || goUpstream.Type != "go" || cfg.Go["default"].Registry != "https://proxy.golang.org" {
		t.Fatalf("unexpected go ecosystem config: %#v / %#v", cfg.Go, goUpstream)
	}
}

func assertLoadedHCLNPMConfig(t *testing.T, cfg config.Config) {
	t.Helper()

	npmUpstream, ok := cfg.NPMUpstream("default")
	if !ok || npmUpstream.Type != "npm" || cfg.NPM["default"].Registry != "https://registry.npmjs.org" {
		t.Fatalf("unexpected npm ecosystem config: %#v / %#v", cfg.NPM, npmUpstream)
	}
	if !npmUpstream.Probe.Enabled || npmUpstream.Probe.Interval != time.Minute ||
		npmUpstream.Probe.Timeout != 2*time.Second ||
		npmUpstream.Probe.Cooldown != 3*time.Minute ||
		npmUpstream.Probe.Jitter != 10*time.Second {
		t.Fatalf("unexpected npm probe config: %#v", npmUpstream.Probe)
	}
}

func assertLoadedHCLPyPIConfig(t *testing.T, cfg config.Config) {
	t.Helper()

	pypiUpstream, ok := cfg.PyPIUpstream("default")
	if !ok || pypiUpstream.Type != "pypi" || cfg.PyPI["default"].Registry != "https://pypi.org" {
		t.Fatalf("unexpected pypi ecosystem config: %#v / %#v", cfg.PyPI, pypiUpstream)
	}
}

func assertLoadedHCLMavenConfig(t *testing.T, cfg config.Config) {
	t.Helper()

	mavenUpstream, ok := cfg.MavenUpstream("central")
	if !ok || mavenUpstream.Type != "maven" || cfg.Maven["central"].Registry != "https://repo.maven.apache.org/maven2" {
		t.Fatalf("unexpected maven ecosystem config: %#v / %#v", cfg.Maven, mavenUpstream)
	}
}

func assertLoadedHCLPolicy(t *testing.T, policyCfg config.PolicyConfig) {
	t.Helper()

	if len(policyCfg.Dependency.Allow) != 1 || len(policyCfg.Dependency.Block) != 1 {
		t.Fatalf("unexpected dependency policy rule counts: %#v", policyCfg.Dependency)
	}
	assertLoadedHCLPolicyRule(t, "allow", policyCfg.Dependency.Allow[0], config.DependencyRuleConfig{
		Ecosystem: "go",
		Alias:     "default",
		Artifact:  "github.com/acme/*",
		Reference: "v1.2.3",
	})
	assertLoadedHCLPolicyRule(t, "block", policyCfg.Dependency.Block[0], config.DependencyRuleConfig{
		Ecosystem: "npm",
		Alias:     "npm",
		Artifact:  "private/*",
		Reference: "*",
	})
}

func assertLoadedHCLPolicyRule(t *testing.T, name string, got, want config.DependencyRuleConfig) {
	t.Helper()

	if got.Ecosystem != want.Ecosystem ||
		got.Alias != want.Alias ||
		got.Artifact != want.Artifact ||
		got.Reference != want.Reference {
		t.Fatalf("policy %s rule = %#v, want %#v", name, got, want)
	}
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

	if !probe.Enabled || probe.Interval != 45*time.Second || probe.Timeout != 4*time.Second ||
		probe.Cooldown != 90*time.Second || probe.Jitter != 7*time.Second {
		t.Fatalf("unexpected probe config: %#v", probe)
	}
}

func assertLoadedHCLWorker(t *testing.T, worker config.WorkerConfig) {
	t.Helper()

	if got := worker; got.ProbeConcurrency != 5 || got.PrefetchConcurrency != 7 || got.LeaseConcurrency != 11 {
		t.Fatalf("unexpected worker config: %#v", got)
	}
}

// Package config_test verifies HCL configuration loading through exported APIs.
package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadHCLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "regimux.hcl")
	if err := os.WriteFile(path, []byte(testHCLConfig()), 0o600); err != nil {
		t.Fatalf("write hcl config: %v", err)
	}

	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load hcl config: %v", err)
	}
	assertLoadedHCLConfig(t, cfg)
}

func testHCLConfig() string {
	return testHCLServer +
		testHCLAuth +
		testHCLCache +
		testHCLScheduler +
		testHCLContainer +
		testHCLDependencyEcosystems +
		testHCLWorker +
		testHCLPolicy
}

const testHCLServer = `
server {
  listen = "127.0.0.1:5555"
}
`

const testHCLAuth = `
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
`

const testHCLCache = `
cache {
  blob {
    small_cache {
      enabled = true
      max_size_bytes = 1024
      ttl = "2h"
    }
  }
}
`

const testHCLContainer = `
container {
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
      jitter = "7s"
    }

    http {
      http2 {
        enabled = true
      }
    }
  }
}
`

const testHCLDependencyEcosystems = `
go {
  default {
    registry = "https://proxy.golang.org"
  }
}

npm {
  default {
    registry = "https://registry.npmjs.org"

    probe {
      enabled = true
      interval = "1m"
      timeout = "2s"
      cooldown = "3m"
      jitter = "10s"
    }
  }
}

pypi {
  default {
    registry = "https://pypi.org"
  }
}

maven {
  central {
    registry = "https://repo.maven.apache.org/maven2"
  }
}
`

const testHCLWorker = `
worker {
  probe_concurrency = 5
  prefetch_concurrency = 7
}
`

const testHCLPolicy = `
policy {
  dependency {
    allow {
      ecosystem = "go"
      alias = "default"
      artifact = "github.com/acme/*"
      reference = " v1.2.3 "
    }

    block {
      ecosystem = "NPM"
      alias = "npm"
      artifact = "private/*"
      reference = " * "
    }
  }
}
`

func assertLoadedHCLConfig(t *testing.T, cfg config.Config) {
	t.Helper()

	if cfg.Server.Listen != "127.0.0.1:5555" {
		t.Fatalf("unexpected listen %q", cfg.Server.Listen)
	}
	assertLoadedHCLAuth(t, cfg.Auth)
	assertLoadedHCLCache(t, cfg.Cache)
	assertLoadedHCLScheduler(t, cfg.Scheduler)
	local, ok := cfg.ContainerUpstream("local")
	if !ok {
		t.Fatal("missing local container upstream")
	}
	assertLoadedHCLUpstream(t, local)
	assertLoadedHCLEcosystemConfig(t, cfg)
	assertLoadedHCLPolicy(t, cfg.Policy)
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
	allow := policyCfg.Dependency.Allow[0]
	block := policyCfg.Dependency.Block[0]
	if want, got := "go", allow.Ecosystem; got != want {
		t.Fatalf("policy allow ecosystem = %q, want %q", got, want)
	}
	if want, got := "default", allow.Alias; got != want {
		t.Fatalf("policy allow alias = %q, want %q", got, want)
	}
	if want, got := "github.com/acme/*", allow.Artifact; got != want {
		t.Fatalf("policy allow artifact = %q, want %q", got, want)
	}
	if want, got := "v1.2.3", allow.Reference; got != want {
		t.Fatalf("policy allow reference = %q, want %q", got, want)
	}

	if want, got := "npm", block.Ecosystem; got != want {
		t.Fatalf("policy block ecosystem = %q, want %q", got, want)
	}
	if want, got := "npm", block.Alias; got != want {
		t.Fatalf("policy block alias = %q, want %q", got, want)
	}
	if want, got := "private/*", block.Artifact; got != want {
		t.Fatalf("policy block artifact = %q, want %q", got, want)
	}
	if want, got := "*", block.Reference; got != want {
		t.Fatalf("policy block reference = %q, want %q", got, want)
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

	if got := worker; got.ProbeConcurrency != 5 || got.PrefetchConcurrency != 7 {
		t.Fatalf("unexpected worker config: %#v", got)
	}
}

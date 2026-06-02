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

cache {
  blob {
    small_cache {
      enabled = true
      max_size_bytes = 1024
      ttl = "2h"
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
      jitter = "7s"
    }

    http {
      http2 {
        enabled = true
      }
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
	assertLoadedHCLCache(t, cfg.Cache)
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

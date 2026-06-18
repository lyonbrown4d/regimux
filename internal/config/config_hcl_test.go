// Package config_test verifies HCL configuration loading through exported APIs.
package config_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

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

func TestLoadHCLDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTestConfigFile(t, dir, "00-server.hcl", `
server {
  listen = "127.0.0.1:1111"
}
`)
	writeTestConfigFile(t, dir, "10-container.hcl", `
container {
  split {
    registry = "https://registry.example.com"
  }
}
`)
	writeTestConfigFile(t, dir, "20-dist.hcl", `
dist {
  assets {
    registry = "https://downloads.example.com"
    allow = ["tools/*"]
  }
}
`)
	writeTestConfigFile(t, dir, "99-server.hcl", `
server {
  listen = "127.0.0.1:2222"
}
`)
	writeTestConfigFile(t, dir, "ignored.txt", `
server {
  listen = "127.0.0.1:3333"
}
`)

	cfg, err := config.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("load hcl config directory: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:2222" {
		t.Fatalf("unexpected directory server.listen %q", cfg.Server.Listen)
	}
	if cfg.Container["split"].Registry != "https://registry.example.com" {
		t.Fatalf("unexpected directory container config: %#v", cfg.Container["split"])
	}
	if cfg.Dist["assets"].Registry != "https://downloads.example.com" || len(cfg.Dist["assets"].Allow) != 1 {
		t.Fatalf("unexpected directory dist config: %#v", cfg.Dist["assets"])
	}
}

func TestLoadHCLDirectoryRejectsEmptyDirectory(t *testing.T) {
	if _, err := config.Load(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected empty config directory error")
	}
}

func writeTestConfigFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
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

    prewarm {
      platforms = ["linux/arm64", "linux/amd64"]
    }

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
  io_concurrency = 9
  lease_concurrency = 11
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
	if !slices.Equal(local.Prewarm.Platforms, []string{"linux/arm64", "linux/amd64"}) {
		t.Fatalf("unexpected container prewarm platforms: %#v", local.Prewarm)
	}
	assertLoadedHCLEcosystemConfig(t, cfg)
	assertLoadedHCLPolicy(t, cfg.Policy)
	assertLoadedHCLWorker(t, cfg.Worker)
}

// Package config_test verifies HCL configuration loading through exported APIs.
package config_test

import (
	"context"
	"os"
	"path/filepath"
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

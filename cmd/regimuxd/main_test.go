package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestBuildAppValidates(t *testing.T) {
	cfg := validTestConfig(t)
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	configPath := writeTestConfig(t, cfg)

	if err := buildApp(configPath).Validate(); err != nil {
		t.Fatalf("validate app graph: %v", err)
	}
}

func TestBuildAppFailsWhenEagerProviderConstructionFails(t *testing.T) {
	cfg := validTestConfig(t)
	blockedParent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockedParent, []byte("x"), 0o600); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}
	cfg.Store.Meta.Path = filepath.Join(blockedParent, "regimux.db")
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	configPath := writeTestConfig(t, cfg)

	_, err := buildApp(configPath).Build()
	if err == nil {
		t.Fatal("expected app build to fail")
	}
	if !strings.Contains(err.Error(), "eager provider failed") {
		t.Fatalf("expected eager provider failure, got: %v", err)
	}
}

func writeTestConfig(t *testing.T, cfg config.Config) string {
	t.Helper()
	escapeHCLPath := func(path string) string {
		return strings.ReplaceAll(path, "\\", "/")
	}

	content := fmt.Sprintf(`
server {
  listen       = "%s"
  public_url   = "%s"
}

log {
  level   = "%s"
  console = %t
}

store {
  meta {
    driver = "%s"
    path   = "%s"
  }

  object {
    driver = "%s"
    path   = "%s"
  }
}

container {
  hub {
    registry = "https://registry-1.docker.io"
    auth {
      type = "anonymous"
    }
  }
}
`, cfg.Server.Listen, cfg.Server.PublicURL, cfg.Log.Level, cfg.Log.Console, cfg.Store.Meta.Driver, escapeHCLPath(cfg.Store.Meta.Path), cfg.Store.Object.Driver, escapeHCLPath(cfg.Store.Object.Path))

	configPath := filepath.Join(t.TempDir(), "regimux.hcl")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

func validTestConfig(t *testing.T) config.Config {
	t.Helper()

	cfg := config.Config{
		Server: config.ServerConfig{
			Listen:    "127.0.0.1:0",
			PublicURL: "http://127.0.0.1",
		},
		Log: config.LogConfig{
			Level:   "info",
			Console: true,
		},
		Store: config.StoreConfig{
			Meta: config.StoreMetaConfig{
				Driver: "sqlite",
				Path:   filepath.Join(t.TempDir(), "regimux.db"),
			},
			Object: config.StoreObjectConfig{
				Driver: "local",
				Path:   t.TempDir(),
			},
		},
		Container: config.ContainerConfig{
			"hub": {
				Registry: "https://registry-1.docker.io",
				Auth: config.AuthConfig{
					Type: "anonymous",
				},
			},
		},
	}
	return cfg
}

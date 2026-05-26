// Package config_test verifies configuration loading through exported APIs.
package config_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
  }
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

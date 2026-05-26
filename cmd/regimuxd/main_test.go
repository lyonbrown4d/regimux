package main

import (
	"log/slog"
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

	logger := slog.New(slog.DiscardHandler)
	if err := buildApp(cfg, logger, "test").Validate(); err != nil {
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

	logger := slog.New(slog.DiscardHandler)
	_, err := buildApp(cfg, logger, "test").Build()
	if err == nil {
		t.Fatal("expected app build to fail")
	}
	if !strings.Contains(err.Error(), "eager provider failed") {
		t.Fatalf("expected eager provider failure, got: %v", err)
	}
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
				Driver: "bboltx",
				Path:   filepath.Join(t.TempDir(), "regimux.db"),
			},
			Object: config.StoreObjectConfig{
				Driver: "local",
				Path:   t.TempDir(),
			},
		},
		Upstreams: map[string]config.UpstreamConfig{
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

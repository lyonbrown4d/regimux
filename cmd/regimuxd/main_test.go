package main

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestBuildAppValidates(t *testing.T) {
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
	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := buildApp(cfg, logger, "test").Validate(); err != nil {
		t.Fatalf("validate app graph: %v", err)
	}
}

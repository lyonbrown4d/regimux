package app

import (
	"io"
	"log/slog"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestApplicationBuildValidates(t *testing.T) {
	cfg := config.Config{
		Server: config.ServerConfig{
			Listen:    "127.0.0.1:0",
			PublicURL: "http://127.0.0.1",
		},
		Log: config.LogConfig{
			Level:   "info",
			Console: true,
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
	if err := New(cfg, logger, "test").build().Validate(); err != nil {
		t.Fatalf("validate app graph: %v", err)
	}
}

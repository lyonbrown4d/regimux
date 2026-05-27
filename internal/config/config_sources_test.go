package config_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

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

func TestLoadCommandLineOverrides(t *testing.T) {
	cfg, err := config.Load(context.Background(), "", "--server.listen=:7777", "--worker.probe_concurrency=7")
	if err != nil {
		t.Fatalf("load with command-line overrides: %v", err)
	}
	if cfg.Server.Listen != ":7777" {
		t.Fatalf("unexpected server.listen %q", cfg.Server.Listen)
	}
	if cfg.Worker.ProbeConcurrency != 7 {
		t.Fatalf("unexpected worker.probe_concurrency %d", cfg.Worker.ProbeConcurrency)
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

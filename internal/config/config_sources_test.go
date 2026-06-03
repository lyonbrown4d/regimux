package config_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

func TestLoadEnvOverridesHCLFile(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{
		"REGIMUX_SERVER__LISTEN",
		"REGIMUX_CACHE__BACKEND",
		"REGIMUX_CACHE__REDIS__ADDRS",
		"REGIMUX_CONTAINER__HUB__REGISTRY",
	} {
		unsetEnv(t, key)
	}

	path := "regimux.hcl"
	if err := os.WriteFile(path, []byte(`
server {
  listen = ":5000"
}

container {
  hub {
    registry = "https://registry-1.docker.io"
  }
}
`), 0o600); err != nil {
		t.Fatalf("write hcl config: %v", err)
	}

	t.Setenv("REGIMUX_SERVER__LISTEN", "127.0.0.1:8888")
	t.Setenv("REGIMUX_CACHE__BACKEND", "redis")
	t.Setenv("REGIMUX_CACHE__REDIS__ADDRS", "redis:6379")
	t.Setenv("REGIMUX_CONTAINER__HUB__REGISTRY", "https://mirror.example.com")

	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load env override config: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:8888" {
		t.Fatalf("unexpected env server.listen %q", cfg.Server.Listen)
	}
	if cfg.Cache.Backend != "redis" {
		t.Fatalf("unexpected env cache.backend %q", cfg.Cache.Backend)
	}
	if len(cfg.Cache.Redis.Addrs) != 1 || cfg.Cache.Redis.Addrs[0] != "redis:6379" {
		t.Fatalf("unexpected env redis addrs %#v", cfg.Cache.Redis.Addrs)
	}
	if cfg.Container["hub"].Registry != "https://mirror.example.com" {
		t.Fatalf("unexpected env container registry %q", cfg.Container["hub"].Registry)
	}
}

func TestLoadDotenvOverridesHCLFile(t *testing.T) {
	t.Chdir(t.TempDir())
	unsetEnv(t, "REGIMUX_SERVER__LISTEN")

	path := "regimux.hcl"
	if err := os.WriteFile(path, []byte(`
server {
  listen = ":5000"
}

container {
  hub {
    registry = "https://registry-1.docker.io"
  }
}
`), 0o600); err != nil {
		t.Fatalf("write hcl config: %v", err)
	}
	if err := os.WriteFile(".env", []byte("REGIMUX_SERVER__LISTEN=127.0.0.1:9999\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		t.Fatalf("load dotenv override config: %v", err)
	}
	if cfg.Server.Listen != "127.0.0.1:9999" {
		t.Fatalf("unexpected dotenv server.listen %q", cfg.Server.Listen)
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

func TestLoadDockerComposeExampleConfigs(t *testing.T) {
	exampleDir := filepath.Join("..", "..", "examples", "compose", "configs")
	for _, name := range []string{"memory.hcl", "redis.hcl", "valkey.hcl"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Load(context.Background(), filepath.Join(exampleDir, name))
			if err != nil {
				t.Fatalf("load example config: %v", err)
			}
			if len(cfg.Container) == 0 {
				t.Fatal("expected at least one container upstream")
			}
		})
	}
}

func TestLoadReleaseExampleConfigs(t *testing.T) {
	configDir := filepath.Join("..", "..", "configs")
	for _, name := range []string{"regimux.minimal.hcl", "regimux.hcl", "regimux.full.hcl"} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Load(context.Background(), filepath.Join(configDir, name))
			if err != nil {
				t.Fatalf("load release config: %v", err)
			}
			if len(cfg.Container) == 0 {
				t.Fatalf("expected ecosystem config: container=%#v", cfg.Container)
			}
		})
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

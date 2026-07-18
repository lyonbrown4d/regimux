package config_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestLoadContainerDefaultAlias(t *testing.T) {
	cfg, err := loadContainerDefaultAliasConfig(t, "hub")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.ContainerDefaultAlias(); got != "hub" {
		t.Fatalf("container default alias = %q, want hub", got)
	}
}

func TestLoadRejectsUnknownContainerDefaultAlias(t *testing.T) {
	_, err := loadContainerDefaultAliasConfig(t, "missing")
	if err == nil {
		t.Fatal("expected unknown container default alias error")
	}
	if !strings.Contains(err.Error(), "default_container_alias") {
		t.Fatalf("error = %q, want default_container_alias context", err)
	}
}

func TestContainerDefaultAliasDefaultsToEmpty(t *testing.T) {
	cfg := loadDefaultConfig(t)
	if got := cfg.ContainerDefaultAlias(); got != "" {
		t.Fatalf("container default alias = %q, want empty", got)
	}
}

func loadContainerDefaultAliasConfig(
	t *testing.T,
	defaultAlias string,
) (config.Config, error) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "regimux.hcl")
	body := "server {\n" +
		"  listen = \"127.0.0.1:8080\"\n" +
		"}\n\n" +
		"default_container_alias = \"" + defaultAlias + "\"\n\n" +
		"container {\n" +
		"  hub {\n" +
		"    registry = \"https://registry.example.com\"\n" +
		"  }\n" +
		"}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.Load(context.Background(), path)
	if err != nil {
		return config.Config{}, fmt.Errorf("load default container alias test config: %w", err)
	}
	return cfg, nil
}

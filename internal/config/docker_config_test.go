package config_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestNormalizeDockerPrewarmRegistryFromPublicURL(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Server.PublicURL = "http://192.168.1.2:5000"
	cfg.Docker.Enabled = true
	cfg.Docker.Prewarm.Enabled = true
	cfg.Docker.Prewarm.Images = []string{" alpine ", "alpine", ""}

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if got := cfg.Docker.Prewarm.Registry; got != "192.168.1.2:5000" {
		t.Fatalf("docker prewarm registry = %q, want %q", got, "192.168.1.2:5000")
	}
	if got := cfg.Docker.Prewarm.Images; len(got) != 1 || got[0] != "alpine" {
		t.Fatalf("docker prewarm images = %#v, want [alpine]", got)
	}
}

func TestValidateDockerPrewarmRejectsUnknownAlias(t *testing.T) {
	t.Parallel()
	cfg := config.DefaultConfig()
	cfg.Docker.Enabled = true
	cfg.Docker.Prewarm.Enabled = true
	cfg.Docker.Prewarm.Alias = "missing"

	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected unknown docker prewarm alias error")
	}
}

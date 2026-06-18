package config_test

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestNormalizeContainerPrewarmDefaultsPlatformForCustomRegistry(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Container = config.ContainerConfig{
		"local": {
			Registry: "https://registry.example.com",
		},
	}

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if got := cfg.Container["local"].Prewarm.Platforms; !slices.Equal(got, []string{config.DefaultContainerPrewarmPlatform()}) {
		t.Fatalf("container prewarm platforms = %#v, want [%s]", got, config.DefaultContainerPrewarmPlatform())
	}
}

func TestDefaultContainerPrewarmPlatformUsesCurrentArchitecture(t *testing.T) {
	if got, wantPrefix := config.DefaultContainerPrewarmPlatform(), "linux/"+runtime.GOARCH; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("DefaultContainerPrewarmPlatform() = %q, want prefix %q", got, wantPrefix)
	}
}

func TestNormalizeContainerPrewarmCanonicalizesPlatforms(t *testing.T) {
	cfg := config.DefaultConfig()
	hub := cfg.Container["hub"]
	hub.Prewarm.Platforms = []string{" linux/ARM64 ", "linux/arm64"}
	cfg.Container["hub"] = hub

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if got := cfg.Container["hub"].Prewarm.Platforms; !slices.Equal(got, []string{"linux/arm64"}) {
		t.Fatalf("container prewarm platforms = %#v, want [linux/arm64]", got)
	}
}

func TestValidateContainerPrewarmRejectsInvalidPlatform(t *testing.T) {
	cfg := config.DefaultConfig()
	hub := cfg.Container["hub"]
	hub.Prewarm.Platforms = []string{"amd64"}
	cfg.Container["hub"] = hub

	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected invalid container prewarm platform error")
	}
}

func TestValidateContainerPrewarmRejectsAllWithSpecificPlatform(t *testing.T) {
	cfg := config.DefaultConfig()
	hub := cfg.Container["hub"]
	hub.Prewarm.Platforms = []string{"all", "linux/amd64"}
	cfg.Container["hub"] = hub

	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected all-exclusive container prewarm platform error")
	}
}

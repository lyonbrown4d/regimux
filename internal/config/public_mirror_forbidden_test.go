package config_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestPublicMirrorForbiddenPolicyRequiresAnonymousContainerUpstream(t *testing.T) {
	cfg := config.DefaultConfig()
	hub := cfg.Container["hub"]
	hub.Mirrors = []string{"https://mirror.example.test"}
	hub.MirrorForbiddenPolicy = "public"
	cfg.Container["hub"] = hub

	if err := cfg.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	upstreamCfg, ok := cfg.ContainerUpstream("hub")
	if !ok {
		t.Fatal("missing hub upstream")
	}
	if upstreamCfg.MirrorForbiddenPolicy != "public" {
		t.Fatalf("MirrorForbiddenPolicy = %q, want public", upstreamCfg.MirrorForbiddenPolicy)
	}
}

func TestPublicMirrorForbiddenPolicyRejectsAuthenticatedContainerUpstream(t *testing.T) {
	cfg := config.DefaultConfig()
	hub := cfg.Container["hub"]
	hub.Mirrors = []string{"https://mirror.example.test"}
	hub.MirrorForbiddenPolicy = "public"
	hub.Auth.Type = "basic"
	cfg.Container["hub"] = hub

	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected authenticated public mirror policy to fail validation")
	}
}

func TestPublicMirrorForbiddenPolicyRejectsUnknownValue(t *testing.T) {
	cfg := config.DefaultConfig()
	hub := cfg.Container["hub"]
	hub.MirrorForbiddenPolicy = "always"
	cfg.Container["hub"] = hub

	if err := cfg.NormalizeAndValidate(); err == nil {
		t.Fatal("expected unknown mirror forbidden policy to fail validation")
	}
}

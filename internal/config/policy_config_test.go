package config_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestNormalizeDependencyPolicyRulesTrimsAndDedupsEmptyRules(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Policy.Dependency = config.DependencyPolicyConfig{
		Allow: []config.DependencyRuleConfig{
			{
				Ecosystem: "  NPM ",
				Alias:     " npmjs ",
				Artifact:  " left-pad ",
				Reference: " metadata ",
			},
			{
				Ecosystem: "   ",
				Alias:     "   ",
				Artifact:  "   ",
				Reference: "   ",
			},
			{
				Ecosystem: "",
				Alias:     "",
				Artifact:  "",
				Reference: "",
			},
		},
		Block: []config.DependencyRuleConfig{
			{
				Ecosystem: "  block ",
				Alias:     "  ",
				Artifact:  " blob ",
				Reference: "  ",
			},
			{
				Ecosystem: "   ",
				Alias:     "   ",
				Artifact:  "   ",
				Reference: "   ",
			},
		},
	}

	if err := cfg.Normalize(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}

	if got := cfg.Policy.Dependency.Allow; len(got) != 1 {
		t.Fatalf("allow len = %d, want 1", len(got))
	} else if got[0] != (config.DependencyRuleConfig{Ecosystem: "npm", Alias: "npmjs", Artifact: "left-pad", Reference: "metadata"}) {
		t.Fatalf("allow[0] = %#v, want trimmed fields", got[0])
	}

	if got := cfg.Policy.Dependency.Block; len(got) != 1 {
		t.Fatalf("block len = %d, want 1", len(got))
	} else if got[0] != (config.DependencyRuleConfig{Ecosystem: "block", Alias: "", Artifact: "blob", Reference: ""}) {
		t.Fatalf("block[0] = %#v, want trimmed fields", got[0])
	}
}

func TestNormalizeDependencyPolicyRulesReturnsNilForEmptyInput(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Policy.Dependency = config.DependencyPolicyConfig{Allow: nil}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	if got := cfg.Policy.Dependency.Allow; got != nil {
		t.Fatalf("allow rules = %#v, want nil", got)
	}

	cfg = config.DefaultConfig()
	cfg.Policy.Dependency = config.DependencyPolicyConfig{Allow: []config.DependencyRuleConfig{}}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	if got := cfg.Policy.Dependency.Allow; got != nil {
		t.Fatalf("allow rules = %#v, want nil", got)
	}
}

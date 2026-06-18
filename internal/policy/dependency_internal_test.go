package policy

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestDependencyRulesFromConfigCopiesFields(t *testing.T) {
	t.Parallel()

	got := dependencyRulesFromConfig([]config.DependencyRuleConfig{
		{
			Ecosystem: "npm",
			Alias:     "npmjs",
			Artifact:  "left-pad",
			Reference: "metadata",
		},
		{
			Ecosystem: "container",
			Alias:     "docker",
			Artifact:  "library/*",
			Reference: "*",
		},
	})

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Ecosystem != "npm" || got[0].Alias != "npmjs" || got[0].Artifact != "left-pad" || got[0].Reference != "metadata" {
		t.Fatalf("got = %#v, want npm rule unchanged", got[0])
	}
	if got[1].Ecosystem != "container" || got[1].Alias != "docker" || got[1].Artifact != "library/*" || got[1].Reference != "*" {
		t.Fatalf("got = %#v, want container rule unchanged", got[1])
	}
}

func TestDependencyRulesFromConfigAllowsNilAndEmpty(t *testing.T) {
	t.Parallel()

	if got := dependencyRulesFromConfig(nil); got != nil && len(got) != 0 {
		t.Fatalf("nil rules = %#v, want empty", got)
	}
	if got := dependencyRulesFromConfig([]config.DependencyRuleConfig{}); len(got) != 0 {
		t.Fatalf("empty rules len = %d, want 0", len(got))
	}
}

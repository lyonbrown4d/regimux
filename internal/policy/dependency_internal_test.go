package policy

import (
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestDependencyRulesFromConfigCopiesFields(t *testing.T) {
	t.Parallel()

	got := dependencyRulesFromConfigValues(dependencyRulesFromConfig(collectionlist.NewList(
		config.DependencyRuleConfig{
			Ecosystem: "npm",
			Alias:     "npmjs",
			Artifact:  "left-pad",
			Reference: "metadata",
		},
		config.DependencyRuleConfig{
			Ecosystem: "container",
			Alias:     "docker",
			Artifact:  "library/*",
			Reference: "*",
		},
	)))
	if got == nil {
		t.Fatal("got = nil, want rules")
	}

	expected := []DependencyRule{
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
	}
	if len(got) != len(expected) {
		t.Fatalf("len = %d, want %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("got[%d] = %#v, want %#v", i, got[i], expected[i])
		}
	}
}

func TestDependencyRulesFromConfigAllowsNilAndEmpty(t *testing.T) {
	t.Parallel()

	if got := dependencyRulesFromConfigValues(dependencyRulesFromConfig(nil)); got != nil {
		t.Fatalf("nil rules = %#v, want nil", got)
	}
	if got := dependencyRulesFromConfigValues(dependencyRulesFromConfig(collectionlist.NewList[config.DependencyRuleConfig]())); got != nil {
		t.Fatalf("nil rules = %#v, want empty", got)
	}
}

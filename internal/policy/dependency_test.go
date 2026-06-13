package policy_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/policy"
)

func TestDependencyPolicyAllowsByDefault(t *testing.T) {
	decision := policy.DependencyPolicy{}.Evaluate(policy.DependencyTarget{
		Ecosystem: "npm",
		Alias:     "npmjs",
		Artifact:  "left-pad",
		Reference: "tarball:left-pad-1.0.0.tgz",
	})
	if !decision.Allowed() {
		t.Fatalf("decision = %#v, want allowed", decision)
	}
}

func TestDependencyPolicyBlockOverridesAllow(t *testing.T) {
	dependencyPolicy := policy.DependencyPolicy{
		Allow: []policy.DependencyRule{{Ecosystem: "npm", Alias: "npmjs", Artifact: "*"}},
		Block: []policy.DependencyRule{{Ecosystem: "npm", Alias: "npmjs", Artifact: "left-pad"}},
	}
	decision := dependencyPolicy.Evaluate(policy.DependencyTarget{
		Ecosystem: "npm",
		Alias:     "npmjs",
		Artifact:  "left-pad",
		Reference: "metadata",
	})
	if decision.Allowed() || decision.Rule.Artifact != "left-pad" {
		t.Fatalf("decision = %#v, want left-pad blocked", decision)
	}
}

func TestDependencyPolicyAllowListBlocksNonMatchingTarget(t *testing.T) {
	dependencyPolicy := policy.DependencyPolicy{
		Allow: []policy.DependencyRule{{Ecosystem: "container", Alias: "hub", Artifact: "library/*"}},
	}
	allowed := dependencyPolicy.Evaluate(policy.DependencyTarget{
		Ecosystem: "container",
		Alias:     "hub",
		Artifact:  "library/nginx",
		Reference: "1.25",
	})
	if !allowed.Allowed() {
		t.Fatalf("allowed decision = %#v, want allowed", allowed)
	}
	blocked := dependencyPolicy.Evaluate(policy.DependencyTarget{
		Ecosystem: "container",
		Alias:     "hub",
		Artifact:  "private/nginx",
		Reference: "1.25",
	})
	if blocked.Allowed() {
		t.Fatalf("blocked decision = %#v, want blocked", blocked)
	}
}

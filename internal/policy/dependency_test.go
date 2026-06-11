package policy

import "testing"

func TestDependencyPolicyAllowsByDefault(t *testing.T) {
	decision := DependencyPolicy{}.Evaluate(DependencyTarget{
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
	policy := DependencyPolicy{
		Allow: []DependencyRule{{Ecosystem: "npm", Alias: "npmjs", Artifact: "*"}},
		Block: []DependencyRule{{Ecosystem: "npm", Alias: "npmjs", Artifact: "left-pad"}},
	}
	decision := policy.Evaluate(DependencyTarget{
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
	policy := DependencyPolicy{
		Allow: []DependencyRule{{Ecosystem: "container", Alias: "hub", Artifact: "library/*"}},
	}
	allowed := policy.Evaluate(DependencyTarget{
		Ecosystem: "container",
		Alias:     "hub",
		Artifact:  "library/nginx",
		Reference: "1.25",
	})
	if !allowed.Allowed() {
		t.Fatalf("allowed decision = %#v, want allowed", allowed)
	}
	blocked := policy.Evaluate(DependencyTarget{
		Ecosystem: "container",
		Alias:     "hub",
		Artifact:  "private/nginx",
		Reference: "1.25",
	})
	if blocked.Allowed() {
		t.Fatalf("blocked decision = %#v, want blocked", blocked)
	}
}

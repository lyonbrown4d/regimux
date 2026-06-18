package config

import "testing"

func TestNormalizeDependencyPolicyRulesTrimsAndDedupsEmptyRules(t *testing.T) {
	t.Parallel()

	got := normalizeDependencyPolicyRules([]DependencyRuleConfig{
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
	})

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Ecosystem != "npm" {
		t.Fatalf("ecosystem = %q, want npm", got[0].Ecosystem)
	}
	if got[0].Alias != "npmjs" || got[0].Artifact != "left-pad" || got[0].Reference != "metadata" {
		t.Fatalf("rule = %#v, want trimmed fields", got[0])
	}
}

func TestNormalizeDependencyPolicyRulesReturnsNilForEmptyInput(t *testing.T) {
	t.Parallel()

	if got := normalizeDependencyPolicyRules(nil); got != nil {
		t.Fatalf("nil rules = %#v, want nil", got)
	}
	if got := normalizeDependencyPolicyRules([]DependencyRuleConfig{}); got != nil {
		t.Fatalf("empty rules = %#v, want nil", got)
	}
}

package config

import "strings"

func (c *Config) normalizePolicy() {
	c.Policy.Dependency.Allow = normalizeDependencyPolicyRules(c.Policy.Dependency.Allow)
	c.Policy.Dependency.Block = normalizeDependencyPolicyRules(c.Policy.Dependency.Block)
}

func normalizeDependencyPolicyRules(rules []DependencyRuleConfig) []DependencyRuleConfig {
	if len(rules) == 0 {
		return nil
	}
	out := make([]DependencyRuleConfig, 0, len(rules))
	for i := range rules {
		rule := DependencyRuleConfig{
			Ecosystem: strings.ToLower(strings.TrimSpace(rules[i].Ecosystem)),
			Alias:     strings.TrimSpace(rules[i].Alias),
			Artifact:  strings.TrimSpace(rules[i].Artifact),
			Reference: strings.TrimSpace(rules[i].Reference),
		}
		if rule.Ecosystem == "" && rule.Alias == "" && rule.Artifact == "" && rule.Reference == "" {
			continue
		}
		out = append(out, rule)
	}
	return out
}

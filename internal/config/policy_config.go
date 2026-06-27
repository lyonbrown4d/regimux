package config

import (
	"strings"

	"github.com/samber/lo"
)

func (c *Config) normalizePolicy() {
	c.Policy.Dependency.Allow = normalizeDependencyPolicyRules(c.Policy.Dependency.Allow)
	c.Policy.Dependency.Block = normalizeDependencyPolicyRules(c.Policy.Dependency.Block)
}

func normalizeDependencyPolicyRules(rules []DependencyRuleConfig) []DependencyRuleConfig {
	if len(rules) == 0 {
		return nil
	}
	normalized := lo.FilterMap(rules, func(rule DependencyRuleConfig, _ int) (DependencyRuleConfig, bool) {
		rule = DependencyRuleConfig{
			Ecosystem: strings.ToLower(strings.TrimSpace(rule.Ecosystem)),
			Alias:     strings.TrimSpace(rule.Alias),
			Artifact:  strings.TrimSpace(rule.Artifact),
			Reference: strings.TrimSpace(rule.Reference),
		}
		return rule, rule.Ecosystem != "" || rule.Alias != "" || rule.Artifact != "" || rule.Reference != ""
	})
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

package config

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
)

func (c *Config) normalizePolicy() {
	c.Policy.Dependency.Allow = normalizeDependencyPolicyRules(c.Policy.Dependency.Allow)
	c.Policy.Dependency.Block = normalizeDependencyPolicyRules(c.Policy.Dependency.Block)
}

func normalizeDependencyPolicyRules(rules []DependencyRuleConfig) []DependencyRuleConfig {
	if len(rules) == 0 {
		return nil
	}
	out := collectionlist.MapList(rules, func(_ int, rule DependencyRuleConfig) DependencyRuleConfig {
		return DependencyRuleConfig{
			Ecosystem: strings.ToLower(strings.TrimSpace(rule.Ecosystem)),
			Alias:     strings.TrimSpace(rule.Alias),
			Artifact:  strings.TrimSpace(rule.Artifact),
			Reference: strings.TrimSpace(rule.Reference),
		}
	})
	rules = collectionlist.FilterList(out, func(_ int, rule DependencyRuleConfig) bool {
		return rule.Ecosystem != "" || rule.Alias != "" || rule.Artifact != "" || rule.Reference != ""
	}).Values()
	if len(rules) == 0 {
		return nil
	}
	return rules
}

package config

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
)

func (c *Config) normalizePolicy() {
	c.Policy.Dependency.Allow = normalizeDependencyPolicyRulesValues(
		normalizeDependencyPolicyRules(collectionlist.NewList(c.Policy.Dependency.Allow...)),
	)
	c.Policy.Dependency.Block = normalizeDependencyPolicyRulesValues(
		normalizeDependencyPolicyRules(collectionlist.NewList(c.Policy.Dependency.Block...)),
	)
}

func normalizeDependencyPolicyRules(rules *collectionlist.List[DependencyRuleConfig]) *collectionlist.List[DependencyRuleConfig] {
	if rules == nil || rules.Len() == 0 {
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
	filtered := collectionlist.FilterList(out, func(_ int, rule DependencyRuleConfig) bool {
		return rule.Ecosystem != "" || rule.Alias != "" || rule.Artifact != "" || rule.Reference != ""
	})
	if filtered == nil || filtered.Len() == 0 {
		return nil
	}
	return filtered
}

func normalizeDependencyPolicyRulesValues(rules *collectionlist.List[DependencyRuleConfig]) []DependencyRuleConfig {
	if rules == nil {
		return nil
	}
	return rules.Values()
}

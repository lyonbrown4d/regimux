package policy

import (
	"errors"
	"fmt"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

const (
	DependencyDecisionAllowed = "allowed"
	DependencyDecisionBlocked = "blocked"
)

var ErrDependencyBlocked = errors.New("dependency request blocked by policy")

type DependencyTarget struct {
	Ecosystem string
	Alias     string
	Artifact  string
	Reference string
}

type DependencyPolicy struct {
	Allow *collectionlist.List[DependencyRule]
	Block *collectionlist.List[DependencyRule]
}

type DependencyRule struct {
	Ecosystem string
	Alias     string
	Artifact  string
	Reference string
}

type DependencyDecision struct {
	Status string
	Rule   DependencyRule
	Target DependencyTarget
}

type DependencyDenyError struct {
	Decision DependencyDecision
}

func (e *DependencyDenyError) Error() string {
	target := e.Decision.Target
	return fmt.Sprintf("dependency policy denied %s/%s/%s:%s", target.Ecosystem, target.Alias, target.Artifact, target.Reference)
}

func (e *DependencyDenyError) Unwrap() error {
	return ErrDependencyBlocked
}

func (p DependencyPolicy) Evaluate(target DependencyTarget) DependencyDecision {
	if rule, ok := firstMatchingDependencyRule(p.Block, target); ok {
		return DependencyDecision{Status: DependencyDecisionBlocked, Rule: rule, Target: target}
	}
	if p.Allow == nil || p.Allow.Len() == 0 {
		return DependencyDecision{Status: DependencyDecisionAllowed, Target: target}
	}
	if rule, ok := firstMatchingDependencyRule(p.Allow, target); ok {
		return DependencyDecision{Status: DependencyDecisionAllowed, Rule: rule, Target: target}
	}
	return DependencyDecision{Status: DependencyDecisionBlocked, Target: target}
}

func (d DependencyDecision) Allowed() bool {
	return d.Status == DependencyDecisionAllowed
}

func (p DependencyPolicy) Check(target DependencyTarget) error {
	decision := p.Evaluate(target)
	if decision.Allowed() {
		return nil
	}
	return &DependencyDenyError{Decision: decision}
}

func FromConfig(cfg config.DependencyPolicyConfig) DependencyPolicy {
	allow := dependencyRulesFromConfig(collectionlist.NewList(cfg.Allow...))
	block := dependencyRulesFromConfig(collectionlist.NewList(cfg.Block...))
	return DependencyPolicy{
		Allow: allow,
		Block: block,
	}
}

func dependencyRulesFromConfig(rules *collectionlist.List[config.DependencyRuleConfig]) *collectionlist.List[DependencyRule] {
	if rules == nil || rules.Len() == 0 {
		return nil
	}
	out := collectionlist.MapList(rules, func(_ int, rule config.DependencyRuleConfig) DependencyRule {
		return DependencyRule{
			Ecosystem: rule.Ecosystem,
			Alias:     rule.Alias,
			Artifact:  rule.Artifact,
			Reference: rule.Reference,
		}
	})
	if out == nil || out.Len() == 0 {
		return nil
	}
	return out
}

func dependencyRulesFromConfigValues(rules *collectionlist.List[DependencyRule]) []DependencyRule {
	if rules == nil {
		return nil
	}
	return rules.Values()
}

func firstMatchingDependencyRule(rules *collectionlist.List[DependencyRule], target DependencyTarget) (DependencyRule, bool) {
	if rules == nil || rules.Len() == 0 {
		return DependencyRule{}, false
	}
	for i := range rules.Len() {
		rule, ok := rules.Get(i)
		if !ok {
			continue
		}
		if dependencyRuleMatches(rule, target) {
			return rule, true
		}
	}
	return DependencyRule{}, false
}

func dependencyRuleMatches(rule DependencyRule, target DependencyTarget) bool {
	return dependencyFieldMatches(rule.Ecosystem, target.Ecosystem) &&
		dependencyFieldMatches(rule.Alias, target.Alias) &&
		dependencyFieldMatches(rule.Artifact, target.Artifact) &&
		dependencyFieldMatches(rule.Reference, target.Reference)
}

func dependencyFieldMatches(pattern, value string) bool {
	return pattern == "" || Match(pattern, value)
}

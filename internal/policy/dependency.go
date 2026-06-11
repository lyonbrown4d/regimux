package policy

import (
	"errors"
	"fmt"

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
	Allow []DependencyRule
	Block []DependencyRule
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
	if len(p.Allow) == 0 {
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
	return DependencyPolicy{
		Allow: dependencyRulesFromConfig(cfg.Allow),
		Block: dependencyRulesFromConfig(cfg.Block),
	}
}

func dependencyRulesFromConfig(rules []config.DependencyRuleConfig) []DependencyRule {
	out := make([]DependencyRule, 0, len(rules))
	for i := range rules {
		out = append(out, DependencyRule{
			Ecosystem: rules[i].Ecosystem,
			Alias:     rules[i].Alias,
			Artifact:  rules[i].Artifact,
			Reference: rules[i].Reference,
		})
	}
	return out
}

func firstMatchingDependencyRule(rules []DependencyRule, target DependencyTarget) (DependencyRule, bool) {
	for i := range rules {
		if dependencyRuleMatches(rules[i], target) {
			return rules[i], true
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

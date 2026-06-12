package observability

import (
	"context"

	"github.com/arcgolabs/observabilityx"
)

type dependencyMetrics struct {
	pulls             observabilityx.Counter
	policyDeniedPulls observabilityx.Counter
}

func newDependencyMetrics(obs observabilityx.Observability) dependencyMetrics {
	return dependencyMetrics{
		pulls: obs.Counter(counterSpec(
			"dependency_proxy_pulls_total",
			"Total dependency proxy pull events.",
			"ecosystem", "kind", "alias", "repository", "status",
		)),
		policyDeniedPulls: obs.Counter(counterSpec(
			"dependency_proxy_policy_denied_pulls_total",
			"Total dependency proxy pulls denied by dependency policy.",
			"ecosystem", "kind", "alias", "repository",
		)),
	}
}

func (m *Metrics) ObserveDependencyPull(ctx context.Context, pull DependencyPullMetric) {
	if m == nil {
		return
	}
	m.dependency.pulls.Add(ctx, 1,
		observabilityx.String("ecosystem", labelOrUnknown(pull.Ecosystem)),
		observabilityx.String("kind", labelOrUnknown(pull.Kind)),
		observabilityx.String("alias", labelOrUnknown(pull.Alias)),
		observabilityx.String("repository", labelOrUnknown(pull.Repository)),
		observabilityx.String("status", labelOrUnknown(pull.Status)),
	)
}

func (m *Metrics) ObserveDependencyPolicyDeniedPull(ctx context.Context, pull DependencyPolicyDeniedPullMetric) {
	if m == nil {
		return
	}
	m.dependency.policyDeniedPulls.Add(ctx, 1,
		observabilityx.String("ecosystem", labelOrUnknown(pull.Ecosystem)),
		observabilityx.String("kind", labelOrUnknown(pull.Kind)),
		observabilityx.String("alias", labelOrUnknown(pull.Alias)),
		observabilityx.String("repository", labelOrUnknown(pull.Repository)),
	)
}

type DependencyPullMetric struct {
	Ecosystem  string
	Kind       string
	Alias      string
	Repository string
	Status     string
}

type DependencyPolicyDeniedPullMetric struct {
	Ecosystem  string
	Kind       string
	Alias      string
	Repository string
}

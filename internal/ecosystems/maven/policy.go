package maven

import (
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
)

func (s *Service) checkDependencyPolicy(requestRoute Route) error {
	if err := accesspolicy.FromConfig(s.cfg.Policy.Dependency).Check(accesspolicy.DependencyTarget{
		Ecosystem: ecosystem.Maven,
		Alias:     requestRoute.Alias,
		Artifact:  requestRoute.Repository,
		Reference: requestRoute.Reference,
	}); err != nil {
		return wrapError(err, "check maven dependency policy")
	}
	return nil
}

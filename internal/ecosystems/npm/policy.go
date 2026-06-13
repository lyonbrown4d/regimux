package npm

import (
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
)

func (s *Service) checkDependencyPolicy(requestRoute route) error {
	if err := accesspolicy.FromConfig(s.cfg.Policy.Dependency).Check(accesspolicy.DependencyTarget{
		Ecosystem: ecosystem.NPM,
		Alias:     requestRoute.Alias,
		Artifact:  requestRoute.Package,
		Reference: requestRoute.Reference,
	}); err != nil {
		return wrapError(err, "check npm dependency policy")
	}
	return nil
}

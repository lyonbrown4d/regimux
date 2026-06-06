package auth

import (
	"strings"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/samber/oops"
)

func (s *Service) AuthorizationForPath(path string, principal any) (authx.AuthorizationModel, error) {
	resolved, err := s.resolvePath(path)
	if err != nil {
		return authx.AuthorizationModel{}, err
	}
	if resolved.Ping {
		return authx.AuthorizationModel{
			Principal: principal,
			Action:    ActionRegistryPing,
			Resource:  "registry",
		}, nil
	}
	return authx.AuthorizationModel{
		Principal: principal,
		Action:    ActionPull,
		Resource:  resolved.Resource,
	}, nil
}

func (s *Service) ResourceFromPath(path string) (string, error) {
	resolved, err := s.resolvePath(path)
	if err != nil {
		return "", err
	}
	return resolved.Resource, nil
}

func (s *Service) ScopeForPath(path string) string {
	resolved, err := s.resolvePath(path)
	if err != nil {
		return ""
	}
	if resolved.Ping || strings.TrimSpace(resolved.Resource) == "" {
		return ""
	}
	return resolved.Scope
}

func (s *Service) resolvePath(path string) (ResolvedResource, error) {
	var resolved ResolvedResource
	var resolvedErr error
	var matched bool
	s.resolvers.Range(func(_ int, resolver ResourceResolver) bool {
		if resolver == nil {
			return true
		}
		value, ok, err := resolver.ResolvePath(path)
		if !ok {
			return true
		}
		matched = true
		if err != nil {
			resolvedErr = oops.With("path", path).Wrapf(err, "resolve auth resource")
			return false
		}
		resolved = value
		return false
	})
	if matched {
		return resolved, resolvedErr
	}
	resolved, matched, resolvedErr = defaultResourceResolver{}.ResolvePath(path)
	if matched {
		return resolved, resolvedErr
	}
	return ResolvedResource{}, oops.In("auth").With("path", path).Errorf("no resource resolver for path")
}

func resourceResolvers(resolvers *collectionlist.List[ResourceResolver]) *collectionlist.List[ResourceResolver] {
	if resolvers == nil || resolvers.Len() == 0 {
		return collectionlist.NewList[ResourceResolver]()
	}
	return resolvers
}

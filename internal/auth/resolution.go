package auth

import (
	"strings"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/samber/oops"
)

func (s *Service) AuthorizationForPath(path string, principal any) (authx.AuthorizationModel, error) {
	resolver, err := s.resolvePath(path)
	if err != nil {
		return authx.AuthorizationModel{}, err
	}
	if resolver.IsPingPath(path) {
		return authx.AuthorizationModel{
			Principal: principal,
			Action:    ActionRegistryPing,
			Resource:  "registry",
		}, nil
	}
	resource, err := resolver.ResourceFromPath(path)
	if err != nil {
		return authx.AuthorizationModel{}, oops.With("path", path).Wrapf(err, "resolve auth resource")
	}
	return authx.AuthorizationModel{
		Principal: principal,
		Action:    ActionPull,
		Resource:  resource,
	}, nil
}

func (s *Service) ResourceFromPath(path string) (string, error) {
	resolver, err := s.resolvePath(path)
	if err != nil {
		return "", err
	}
	resource, err := resolver.ResourceFromPath(path)
	if err != nil {
		return "", oops.With("path", path).Wrapf(err, "resolve auth resource")
	}
	return resource, nil
}

func (s *Service) ScopeForPath(path string) string {
	resolver, err := s.resolvePath(path)
	if err != nil {
		return ""
	}
	if resolver.IsPingPath(path) {
		return ""
	}
	resource, err := resolver.ResourceFromPath(path)
	if err != nil || strings.TrimSpace(resource) == "" {
		return ""
	}
	return resolver.ScopeFromResource(resource)
}

func (s *Service) resolvePath(path string) (ResourceResolver, error) {
	for _, resolver := range s.resolversOrFallback().Values() {
		if resolver != nil && resolver.MatchPath(path) {
			return resolver, nil
		}
	}
	if isRegistryPingPath(path) {
		return defaultResourceResolver{}, nil
	}
	return nil, oops.In("auth").With("path", path).Errorf("no resource resolver for path")
}

func (s *Service) resolversOrFallback() *collectionlist.List[ResourceResolver] {
	if s.resolvers == nil || s.resolvers.Len() == 0 {
		return collectionlist.NewList[ResourceResolver]()
	}
	return s.resolvers
}

package auth

type ResourceResolver interface {
	ResolvePath(path string) (ResolvedResource, bool, error)
}

type ResolvedResource struct {
	Ping     bool
	Resource string
	Scope    string
}

type defaultResourceResolver struct{}

func (defaultResourceResolver) ResolvePath(path string) (ResolvedResource, bool, error) {
	if !isRegistryPingPath(path) {
		return ResolvedResource{}, false, nil
	}
	return ResolvedResource{Ping: true, Resource: "registry"}, true, nil
}

func RepositoryPullResource(resource string) ResolvedResource {
	return ResolvedResource{
		Resource: resource,
		Scope:    ScopeTypeRepository + ":" + resource + ":" + ActionPull,
	}
}

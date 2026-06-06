package auth

type ResourceResolver interface {
	MatchPath(path string) bool
	IsPingPath(path string) bool
	ResourceFromPath(path string) (string, error)
	ScopeFromResource(resource string) string
}

type defaultResourceResolver struct{}

func (defaultResourceResolver) MatchPath(path string) bool {
	return isRegistryPingPath(path)
}

func (defaultResourceResolver) IsPingPath(path string) bool {
	return isRegistryPingPath(path)
}

func (defaultResourceResolver) ResourceFromPath(path string) (string, error) {
	return "", nil
}

func (defaultResourceResolver) ScopeFromResource(resource string) string {
	return ""
}

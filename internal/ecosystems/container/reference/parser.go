package reference

import "strings"

type RouteKind string

const (
	RoutePing      RouteKind = "ping"
	RouteManifest  RouteKind = "manifest"
	RouteBlob      RouteKind = "blob"
	RouteTags      RouteKind = "tags"
	RouteReferrers RouteKind = "referrers"
)

type Route struct {
	Kind      RouteKind
	Alias     string
	Repo      string
	Reference string
	Digest    string
}

func (r Route) MirrorRepo() string {
	if r.Alias == "" {
		return r.Repo
	}
	if r.Repo == "" {
		return r.Alias
	}
	return r.Alias + "/" + r.Repo
}

func (r Route) WithDefaultNamespace(namespace string) Route {
	namespace = strings.Trim(strings.TrimSpace(namespace), "/")
	if namespace == "" || r.Repo == "" || strings.Contains(r.Repo, "/") {
		return r
	}
	r.Repo = namespace + "/" + r.Repo
	return r
}

func Parse(path string) (Route, error) {
	route, err := ParsePath(path)
	if err != nil {
		return Route{}, err
	}
	return *route, nil
}

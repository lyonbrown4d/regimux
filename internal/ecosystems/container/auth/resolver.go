package containerauth

import (
	"strings"

	"github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/samber/oops"
)

type Resolver struct {
	cfg config.Config
}

func NewResourceResolver(cfg config.Config) auth.ResourceResolver {
	return &Resolver{cfg: cfg}
}

func (r *Resolver) ResolvePath(path string) (auth.ResolvedResource, bool, error) {
	cleanPath := strings.TrimSpace(path)
	if !strings.HasPrefix(cleanPath, "/v2") {
		return auth.ResolvedResource{}, false, nil
	}
	route, err := reference.Parse(cleanPath)
	if err != nil {
		return auth.ResolvedResource{}, true, oops.Wrapf(err, "parse container auth resource")
	}
	if route.Kind == reference.RoutePing {
		return auth.ResolvedResource{Ping: true, Resource: "registry"}, true, nil
	}
	if route.Repo == "" || route.Alias == "" {
		return auth.ResolvedResource{}, true, oops.In("auth").Errorf("path is not a pullable container resource")
	}
	repo := route.Repo
	if upstreamCfg, ok := r.cfg.ContainerUpstream(route.Alias); ok && strings.TrimSpace(upstreamCfg.DefaultNamespace) != "" {
		repo = route.WithDefaultNamespace(upstreamCfg.DefaultNamespace).Repo
	}
	resource := route.Alias + "/" + strings.Trim(repo, "/")
	return auth.RepositoryPullResource(resource), true, nil
}

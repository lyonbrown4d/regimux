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

func (r *Resolver) MatchPath(path string) bool {
	_, err := reference.Parse(strings.TrimSpace(path))
	return err == nil
}

func (r *Resolver) IsPingPath(path string) bool {
	route, err := reference.Parse(strings.TrimSpace(path))
	if err != nil {
		return false
	}
	return route.Kind == reference.RoutePing
}

func (r *Resolver) ResourceFromPath(path string) (string, error) {
	route, err := reference.Parse(strings.TrimSpace(path))
	if err != nil {
		return "", oops.Wrapf(err, "parse container auth resource")
	}
	if route.Kind == reference.RoutePing || route.Repo == "" || route.Alias == "" {
		return "", oops.In("auth").Errorf("path is not a pullable container resource")
	}
	repo := route.Repo
	if upstreamCfg, ok := r.cfg.ContainerUpstream(route.Alias); ok && strings.TrimSpace(upstreamCfg.DefaultNamespace) != "" {
		repo = route.WithDefaultNamespace(upstreamCfg.DefaultNamespace).Repo
	}
	return route.Alias + "/" + strings.Trim(repo, "/"), nil
}

func (r *Resolver) ScopeFromResource(resource string) string {
	return auth.ScopeTypeRepository + ":" + strings.Trim(resource, "/") + ":" + auth.ActionPull
}

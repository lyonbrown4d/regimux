package admin

import (
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/samber/oops"
)

func (s *Service) syncRoute(form SyncForm, repo string) (*reference.Route, SyncForm, error) {
	if form.Ecosystem != ecosystem.Container {
		return nil, form, nil
	}
	routing, err := reference.ParseManifestPath("/v2/" + form.Alias + "/" + repo + "/manifests/" + form.Reference)
	if err != nil {
		return nil, form, oops.In("admin").Wrapf(err, "invalid sync target")
	}
	upstreamCfg, ok := s.cfg.ContainerUpstream(routing.Alias)
	if !ok {
		return nil, form, oops.In("admin").With("alias", routing.Alias).Errorf("unknown upstream alias %q", routing.Alias)
	}
	*routing = routing.WithDefaultNamespace(upstreamCfg.DefaultNamespace)
	form.Repository = routing.Repo
	form.Reference = routing.Reference
	return routing, form, nil
}

func (s *Service) syncUpstream(ecosystemName, alias string) (config.UpstreamConfig, bool) {
	switch strings.TrimSpace(ecosystemName) {
	case ecosystem.Container:
		return s.cfg.ContainerUpstream(alias)
	case ecosystem.Go:
		return s.cfg.GoUpstream(alias)
	case ecosystem.NPM:
		return s.cfg.NPMUpstream(alias)
	case ecosystem.PyPI:
		return s.cfg.PyPIUpstream(alias)
	case ecosystem.Maven:
		return s.cfg.MavenUpstream(alias)
	default:
		return config.UpstreamConfig{}, false
	}
}

func routeToSyncAlias(alias string, route *reference.Route) string {
	if route == nil {
		return alias
	}
	return route.Alias
}

func routeToSyncRepo(route *reference.Route, fallback string) string {
	if route == nil {
		return fallback
	}
	return route.Repo
}

func routeToSyncReference(route *reference.Route, fallback string) string {
	if route == nil {
		return fallback
	}
	return route.Reference
}

func defaultSyncForm() SyncForm {
	return SyncForm{
		UpstreamAlias: "container:hub",
		Reference:     "latest",
	}
}

func firstConfiguredTarget(_ config.Config, runtime string, upstreams *collectionmapping.OrderedMap[string, config.UpstreamConfig]) (string, bool) {
	if upstreams == nil || upstreams.Len() == 0 {
		return "", false
	}
	var value string
	upstreams.Range(func(alias string, _ config.UpstreamConfig) bool {
		value = syncTargetValue(runtime, alias)
		return false
	})
	return value, value != ""
}

func defaultSyncUpstreamValue(cfg config.Config) string {
	targets := []struct {
		name      string
		upstreams *collectionmapping.OrderedMap[string, config.UpstreamConfig]
	}{
		{name: ecosystem.Container, upstreams: cfg.OrderedContainerUpstreams()},
		{name: ecosystem.Go, upstreams: cfg.OrderedGoUpstreams()},
		{name: ecosystem.NPM, upstreams: cfg.OrderedNPMUpstreams()},
		{name: ecosystem.PyPI, upstreams: cfg.OrderedPyPIUpstreams()},
		{name: ecosystem.Maven, upstreams: cfg.OrderedMavenUpstreams()},
	}
	for _, target := range targets {
		if value, ok := firstConfiguredTarget(cfg, target.name, target.upstreams); ok {
			return value
		}
	}
	return ""
}

package admin

import (
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

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

func defaultSyncForm() SyncForm {
	return SyncForm{
		Reference: "latest",
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

package maven

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

const resolvedUpstreamHeader = "X-Regimux-Upstream"

func (s *Service) group(alias string) (config.MavenGroupConfig, bool) {
	return s.cfg.MavenGroup(alias)
}

func (s *Service) groupUpstream(alias string) (config.UpstreamConfig, bool) {
	group, ok := s.group(alias)
	if !ok || len(group.Members) == 0 {
		return config.UpstreamConfig{}, false
	}
	upstream, ok := s.cfg.MavenUpstream(group.Members[0])
	if !ok {
		return config.UpstreamConfig{}, false
	}
	upstream.Alias = strings.TrimSpace(alias)
	upstream.Registry = ""
	upstream.Mirrors = nil
	upstream.MirrorPolicy = ""
	return upstream, true
}

// GroupAliases returns logical Maven group aliases in deterministic order.
func (s *Service) GroupAliases() []string {
	return s.cfg.OrderedMavenGroups()
}

// Targets returns physical Maven upstreams plus logical Maven groups.
func (s *Service) Targets() *collectionlist.List[ecosystem.Upstream] {
	upstreams := s.Upstreams().Values()
	targets := make([]ecosystem.Upstream, 0, len(upstreams)+len(s.GroupAliases()))
	for index := range upstreams {
		upstream := &upstreams[index]
		targets = append(targets, ecosystem.Upstream{
			Ecosystem: "maven",
			Alias:     upstream.Alias,
			Config:    upstream.Config,
		})
	}
	for _, alias := range s.GroupAliases() {
		upstream, ok := s.groupUpstream(alias)
		if !ok {
			continue
		}
		targets = append(targets, ecosystem.Upstream{
			Ecosystem: "maven",
			Alias:     alias,
			Config:    upstream,
		})
	}
	return collectionlist.NewList(targets...)
}

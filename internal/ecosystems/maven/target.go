package maven

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/lo"
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
	targets := collectionlist.MapList(s.Upstreams(), func(_ int, upstream Upstream) ecosystem.Upstream {
		return ecosystem.Upstream{
			Ecosystem: "maven",
			Alias:     upstream.Alias,
			Config:    upstream.Config,
		}
	})
	groups := lo.FilterMap(s.GroupAliases(), func(alias string, _ int) (ecosystem.Upstream, bool) {
		upstream, ok := s.groupUpstream(alias)
		return ecosystem.Upstream{
			Ecosystem: "maven",
			Alias:     alias,
			Config:    upstream,
		}, ok
	})
	return targets.MergeSlice(groups)
}

package admin_test

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type fakeUpstreamRuntime struct {
	name      string
	upstreams *collectionlist.List[ecosystem.Upstream]
}

func (r fakeUpstreamRuntime) Name() string {
	return r.name
}

func (r fakeUpstreamRuntime) Upstreams() *collectionlist.List[ecosystem.Upstream] {
	return r.upstreams
}

func newAdminTestRuntimes(cfg config.Config) *collectionlist.List[ecosystem.Runtime] {
	return collectionlist.NewList[ecosystem.Runtime](
		fakeUpstreamRuntime{name: ecosystem.Container, upstreams: upstreamListFromMap(ecosystem.Container, cfg.OrderedContainerUpstreams())},
		fakeUpstreamRuntime{name: ecosystem.Go, upstreams: upstreamListFromMap(ecosystem.Go, cfg.OrderedGoUpstreams())},
		fakeUpstreamRuntime{name: ecosystem.NPM, upstreams: upstreamListFromMap(ecosystem.NPM, cfg.OrderedNPMUpstreams())},
		fakeUpstreamRuntime{name: ecosystem.PyPI, upstreams: upstreamListFromMap(ecosystem.PyPI, cfg.OrderedPyPIUpstreams())},
		fakeUpstreamRuntime{name: ecosystem.Maven, upstreams: upstreamListFromMap(ecosystem.Maven, cfg.OrderedMavenUpstreams())},
	)
}

func upstreamListFromMap(ecosystemName string, upstreams interface {
	Range(func(string, config.UpstreamConfig) bool)
	Len() int
}) *collectionlist.List[ecosystem.Upstream] {
	out := collectionlist.NewListWithCapacity[ecosystem.Upstream](upstreams.Len())
	upstreams.Range(func(alias string, upstreamCfg config.UpstreamConfig) bool {
		out.Add(ecosystem.Upstream{
			Ecosystem: ecosystemName,
			Alias:     alias,
			Config:    upstreamCfg,
		})
		return true
	})
	return out
}

package pypiproxy

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type runtimeAdapter struct {
	service *Service
}

func newRuntimeAdapter(service *Service) *runtimeAdapter {
	return &runtimeAdapter{service: service}
}

func (r *runtimeAdapter) Name() string {
	return ecosystemPyPI
}

func (r *runtimeAdapter) Upstreams() *collectionlist.List[ecosystem.Upstream] {
	if r == nil || r.service == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	upstreams := r.service.Upstreams()
	out := make([]ecosystem.Upstream, 0, len(upstreams))
	for i := range upstreams {
		out = append(out, ecosystem.Upstream{
			Ecosystem: r.Name(),
			Alias:     upstreams[i].Alias,
			Config:    upstreams[i].Config,
		})
	}
	return collectionlist.NewList(out...)
}

func (r *runtimeAdapter) UpstreamAliases() *collectionlist.List[string] {
	return ecosystem.UpstreamAliases(r.Upstreams())
}

func (r *runtimeAdapter) ProbeCapability() ecosystem.Capability {
	return ecosystem.UnsupportedCapability("pypi proxy probe is not implemented", r.Upstreams())
}

func (r *runtimeAdapter) PrefetchCapability() ecosystem.Capability {
	return ecosystem.UnsupportedCapability("pypi proxy prefetch is not implemented", r.Upstreams())
}

var _ ecosystem.Runtime = (*runtimeAdapter)(nil)
var _ ecosystem.UpstreamProvider = (*runtimeAdapter)(nil)
var _ ecosystem.UpstreamAliasProvider = (*runtimeAdapter)(nil)
var _ ecosystem.CapabilityProvider = (*runtimeAdapter)(nil)

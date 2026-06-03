package ecosystem

import (
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type ConfigRuntime struct {
	name      string
	upstreams *collectionlist.List[Upstream]
}

func NewConfigRuntime(name string, upstreams *collectionmapping.OrderedMap[string, config.UpstreamConfig]) *ConfigRuntime {
	values := collectionlist.NewList[Upstream]()
	if upstreams != nil {
		upstreams.Range(func(alias string, cfg config.UpstreamConfig) bool {
			values.Add(Upstream{
				Ecosystem: name,
				Alias:     alias,
				Config:    cfg,
			})
			return true
		})
	}
	return &ConfigRuntime{
		name:      name,
		upstreams: values,
	}
}

func (r *ConfigRuntime) Name() string {
	if r == nil {
		return ""
	}
	return r.name
}

func (r *ConfigRuntime) Upstreams() *collectionlist.List[Upstream] {
	if r == nil || r.upstreams == nil {
		return collectionlist.NewList[Upstream]()
	}
	return r.upstreams
}

func (r *ConfigRuntime) UpstreamAliases() *collectionlist.List[string] {
	return UpstreamAliases(r.Upstreams())
}

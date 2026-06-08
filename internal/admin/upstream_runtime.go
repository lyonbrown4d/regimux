package admin

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

func (s *Service) configuredUpstreams() *collectionlist.List[ecosystem.Upstream] {
	if s == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	return configuredUpstreamsFromRuntimes(s.runtimes)
}

func configuredUpstreamsFromRuntimes(runtimes *collectionlist.List[ecosystem.Runtime]) *collectionlist.List[ecosystem.Upstream] {
	out := collectionlist.NewList[ecosystem.Upstream]()
	if runtimes == nil {
		return out
	}
	runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		provider, ok := runtime.(ecosystem.UpstreamProvider)
		if !ok || provider == nil {
			return true
		}
		name := strings.TrimSpace(runtime.Name())
		upstreams := provider.Upstreams()
		if upstreams == nil {
			return true
		}
		upstreams.Range(func(_ int, upstream ecosystem.Upstream) bool {
			upstream.Ecosystem = upstreamEcosystem(name, upstream.Ecosystem)
			if upstream.Ecosystem != "" && strings.TrimSpace(upstream.Alias) != "" {
				out.Add(upstream)
			}
			return true
		})
		return true
	})
	return out
}

func upstreamEcosystem(runtimeName, upstreamName string) string {
	upstreamName = strings.TrimSpace(upstreamName)
	if upstreamName != "" {
		return upstreamName
	}
	return strings.TrimSpace(runtimeName)
}

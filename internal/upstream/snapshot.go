package upstream

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
)

type ClientSnapshot struct {
	Upstreams []UpstreamSnapshot
}

type UpstreamSnapshot struct {
	Alias      string
	Policy     string
	BlobPolicy string
	Endpoints  []EndpointSnapshot
}

type EndpointSnapshot struct {
	Registry string
	Role     string
	Health   EndpointHealthSnapshot
}

func (c *Client) Snapshot(now time.Time) ClientSnapshot {
	if c == nil || c.upstreams == nil {
		return ClientSnapshot{}
	}

	return ClientSnapshot{
		Upstreams: collectionlist.FilterMapList(
			collectionlist.NewList(c.upstreams.Values()...),
			func(_ int, pool *upstreamPool) (UpstreamSnapshot, bool) {
				if pool == nil {
					return UpstreamSnapshot{}, false
				}
				return pool.snapshot(now), true
			},
		).Values(),
	}
}

func (p *upstreamPool) snapshot(now time.Time) UpstreamSnapshot {
	p.mu.Lock()
	alias := p.alias
	policy := p.policy
	blobPolicy := p.blobPolicy
	runtimes := p.runtimes
	if runtimes == nil {
		runtimes = collectionlist.NewList[upstreamRuntime]()
	}
	p.mu.Unlock()

	out := UpstreamSnapshot{
		Alias:      alias,
		Policy:     policy,
		BlobPolicy: blobPolicy,
	}
	out.Endpoints = collectionlist.MapList(runtimes, func(i int, runtime upstreamRuntime) EndpointSnapshot {
		registry := normalizeEndpointHealthRegistry(runtime.config.Registry)
		return EndpointSnapshot{
			Registry: registry,
			Role:     endpointRole(i, runtimes.Len()),
			Health:   p.health.Snapshot(registry, now),
		}
	}).Values()
	return out
}

func endpointRole(index, total int) string {
	if index == total-1 {
		return "primary"
	}
	return "mirror"
}

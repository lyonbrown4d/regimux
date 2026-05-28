package upstream

import "time"

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

	out := ClientSnapshot{}
	c.upstreams.Range(func(_ string, pool *upstreamPool) bool {
		if pool != nil {
			out.Upstreams = append(out.Upstreams, pool.snapshot(now))
		}
		return true
	})
	return out
}

func (p *upstreamPool) snapshot(now time.Time) UpstreamSnapshot {
	p.mu.Lock()
	alias := p.alias
	policy := p.policy
	blobPolicy := p.blobPolicy
	runtimes := append([]upstreamRuntime(nil), p.runtimes...)
	p.mu.Unlock()

	out := UpstreamSnapshot{
		Alias:      alias,
		Policy:     policy,
		BlobPolicy: blobPolicy,
		Endpoints:  make([]EndpointSnapshot, 0, len(runtimes)),
	}
	for i := range runtimes {
		runtime := &runtimes[i]
		registry := normalizeEndpointHealthRegistry(runtime.config.Registry)
		out.Endpoints = append(out.Endpoints, EndpointSnapshot{
			Registry: registry,
			Role:     endpointRole(i, len(runtimes)),
			Health:   p.health.Snapshot(registry, now),
		})
	}
	return out
}

func endpointRole(index, total int) string {
	if index == total-1 {
		return "primary"
	}
	return "mirror"
}

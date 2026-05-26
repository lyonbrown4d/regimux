package upstream

import (
	"log/slog"
	"strings"
	"sync"

	clienthttp "github.com/arcgolabs/clientx/http"
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionset "github.com/arcgolabs/collectionx/set"
)

type upstreamPool struct {
	mu       sync.Mutex
	alias    string
	policy   string
	runtimes []upstreamRuntime
	next     int
}

type upstreamRuntime struct {
	config Config
	client clienthttp.Client
	err    error
}

func newUpstreamPool(cfg Config, logger *slog.Logger) *upstreamPool {
	pool := &upstreamPool{
		alias:  cfg.Alias,
		policy: normalizeMirrorPolicy(cfg.MirrorPolicy),
	}
	for _, registry := range endpointRegistries(cfg) {
		runtimeCfg := cfg
		runtimeCfg.Registry = registry
		runtime := upstreamRuntime{config: runtimeCfg}
		runtime.client, runtime.err = newHTTPClient(runtimeCfg)
		if runtime.err != nil && logger != nil {
			logger.Warn(
				"create upstream http client failed",
				"alias", cfg.Alias,
				"registry", registry,
				"error", runtime.err,
			)
		}
		pool.runtimes = append(pool.runtimes, runtime)
	}
	return pool
}

func endpointRegistries(cfg Config) []string {
	registries := collectionset.NewOrderedSetWithCapacity[string](len(cfg.Mirrors) + 1)
	for _, registry := range cfg.Mirrors {
		registry = strings.TrimRight(strings.TrimSpace(registry), "/")
		if registry == "" {
			continue
		}
		registries.Add(registry)
	}
	registry := strings.TrimRight(strings.TrimSpace(cfg.Registry), "/")
	if registry != "" {
		registries.Add(registry)
	}
	return registries.Values()
}

func normalizeMirrorPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "round_robin":
		return "round_robin"
	default:
		return "ordered"
	}
}

func (p *upstreamPool) runtimesForAttempt() []upstreamRuntime {
	if p == nil {
		return nil
	}
	if len(p.runtimes) <= 1 || p.policy != "round_robin" {
		return p.runtimes
	}

	start := p.nextOffset()
	out := collectionlist.NewListWithCapacity[upstreamRuntime](len(p.runtimes))
	for i := range p.runtimes {
		out.Add(p.runtimes[(start+i)%len(p.runtimes)])
	}
	return out.Values()
}

func (p *upstreamPool) nextOffset() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	start := p.next
	p.next = (p.next + 1) % len(p.runtimes)
	return start
}

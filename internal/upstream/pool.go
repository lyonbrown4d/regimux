package upstream

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	clienthttp "github.com/arcgolabs/clientx/http"
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

const (
	mirrorPolicyOrdered    = "ordered"
	mirrorPolicyRoundRobin = "round_robin"
	mirrorPolicyLatency    = "latency"
)

type upstreamPool struct {
	mu          sync.Mutex
	alias       string
	policy      string
	blobPolicy  string
	blobTopN    int
	blobLimit   int
	blobMaxAttempts int
	runtimes    []upstreamRuntime
	next        int
	nextBlob    int
	limiters    map[string]chan struct{}
	health      *EndpointHealthTracker
	probeConfig ProbeConfig
}

type upstreamRuntime struct {
	config Config
	client clienthttp.Client
	err    error
}

func newUpstreamPool(cfg Config, logger *slog.Logger) *upstreamPool {
	policy := normalizeMirrorPolicy(cfg.MirrorPolicy)
	pool := &upstreamPool{
		alias:       cfg.Alias,
		policy:      policy,
		blobPolicy:  normalizeBlobMirrorPolicy(cfg.Blob.MirrorPolicy, policy),
		blobTopN:    cfg.Blob.TopN,
		blobLimit:   cfg.Blob.MaxConcurrencyPerEndpoint,
		blobMaxAttempts: cfg.Blob.MaxConcurrentAttempts,
		probeConfig: cfg.Probe,
		health: NewEndpointHealthTracker(EndpointHealthOptions{
			Cooldown: cfg.Probe.Cooldown,
		}),
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
	if logger != nil {
		logger.Debug(
			"upstream pool initialized",
			"alias", cfg.Alias,
			"endpoint_count", len(pool.runtimes),
			"mirror_policy", pool.policy,
			"blob_mirror_policy", pool.blobPolicy,
			"blob_top_n", pool.blobTopN,
			"blob_max_concurrency_per_endpoint", pool.blobLimit,
			"blob_max_concurrent_attempts", pool.blobMaxAttempts,
			"probe_enabled", pool.probeConfig.Enabled,
			"probe_interval", pool.probeConfig.Interval,
			"probe_timeout", pool.probeConfig.Timeout,
			"probe_cooldown", pool.probeConfig.Cooldown,
		)
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
	case mirrorPolicyRoundRobin:
		return mirrorPolicyRoundRobin
	default:
		return mirrorPolicyOrdered
	}
}

func normalizeBlobMirrorPolicy(policy, fallback string) string {
	policy = strings.ToLower(strings.TrimSpace(policy))
	switch policy {
	case mirrorPolicyLatency:
		return mirrorPolicyLatency
	case mirrorPolicyRoundRobin:
		return mirrorPolicyRoundRobin
	case mirrorPolicyOrdered, "failover":
		return mirrorPolicyOrdered
	case "":
		return normalizeMirrorPolicy(fallback)
	default:
		return normalizeMirrorPolicy(fallback)
	}
}

func (p *upstreamPool) runtimesForOperation(operation string) []upstreamRuntime {
	if p == nil {
		return nil
	}
	if operation == "blob" {
		return p.blobRuntimes()
	}
	return p.runtimesForPolicy(p.policy, false)
}

func (p *upstreamPool) blobRuntimes() []upstreamRuntime {
	switch p.blobPolicy {
	case mirrorPolicyRoundRobin:
		return p.runtimesForPolicy(mirrorPolicyRoundRobin, true)
	case mirrorPolicyLatency:
		return p.latencyRuntimes()
	default:
		return p.runtimesForPolicy(mirrorPolicyOrdered, true)
	}
}

func (p *upstreamPool) runtimesForPolicy(policy string, blob bool) []upstreamRuntime {
	if len(p.runtimes) <= 1 || policy != mirrorPolicyRoundRobin {
		return p.runtimes
	}

	start := p.nextOffset(len(p.runtimes), blob)
	out := collectionlist.NewListWithCapacity[upstreamRuntime](len(p.runtimes))
	for i := range p.runtimes {
		out.Add(p.runtimes[(start+i)%len(p.runtimes)])
	}
	return out.Values()
}

func (p *upstreamPool) latencyRuntimes() []upstreamRuntime {
	if len(p.runtimes) <= 1 {
		return p.runtimes
	}
	ranked := p.health.rankRuntimes(p.runtimes, time.Now())
	topN := p.blobTopN
	if topN <= 0 || topN > len(ranked) {
		topN = len(ranked)
	}
	if topN <= 1 {
		return ranked
	}

	start := p.nextOffset(topN, true)
	out := collectionlist.NewListWithCapacity[upstreamRuntime](len(ranked))
	for i := 0; i < topN; i++ {
		out.Add(ranked[(start+i)%topN])
	}
	for i := topN; i < len(ranked); i++ {
		out.Add(ranked[i])
	}
	return out.Values()
}

func (p *upstreamPool) nextOffset(modulo int, blob bool) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if modulo <= 0 {
		return 0
	}
	if blob {
		start := p.nextBlob
		p.nextBlob = (p.nextBlob + 1) % modulo
		return start
	}

	start := p.next
	p.next = (p.next + 1) % modulo
	return start
}

func (p *upstreamPool) acquireRuntime(ctx context.Context, operation string, runtime upstreamRuntime) (func(), error) {
	if p == nil || operation != "blob" {
		return func() {}, nil
	}

	releaseHealth := p.health.Acquire(runtime.config.Registry)
	limiter := p.limiter(runtime.config.Registry)
	if limiter == nil {
		return releaseHealth, nil
	}

	select {
	case limiter <- struct{}{}:
		return func() {
			<-limiter
			releaseHealth()
		}, nil
	case <-ctx.Done():
		releaseHealth()
		return nil, distribution.ErrUpstream.WithDetail(ctx.Err().Error())
	}
}

func (p *upstreamPool) limiter(registry string) chan struct{} {
	if p == nil || p.blobLimit <= 0 {
		return nil
	}
	registry = normalizeEndpointHealthRegistry(registry)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.limiters == nil {
		p.limiters = make(map[string]chan struct{})
	}
	limiter := p.limiters[registry]
	if limiter == nil {
		limiter = make(chan struct{}, p.blobLimit)
		p.limiters[registry] = limiter
	}
	return limiter
}

func (p *upstreamPool) recordProbeSuccess(runtime upstreamRuntime, latency time.Duration) {
	if p == nil {
		return
	}
	p.health.RecordProbeSuccess(runtime.config.Registry, latency, time.Now())
}

func (p *upstreamPool) recordProbeFailure(runtime upstreamRuntime) {
	if p == nil {
		return
	}
	p.health.RecordProbeFailure(runtime.config.Registry, time.Now())
}

func (p *upstreamPool) probeEnabled() bool {
	return p != nil && p.probeConfig.Enabled
}

func (p *upstreamPool) blobAttemptConcurrency() int {
	if p == nil || p.blobMaxAttempts <= 0 {
		return 1
	}
	return p.blobMaxAttempts
}

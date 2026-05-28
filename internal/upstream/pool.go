package upstream

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	clienthttp "github.com/arcgolabs/clientx/http"
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

const (
	mirrorPolicyOrdered    = "ordered"
	mirrorPolicyRoundRobin = "round_robin"
	mirrorPolicyLatency    = "latency"
)

type upstreamPool struct {
	mu              sync.Mutex
	alias           string
	policy          string
	blobPolicy      string
	blobTopN        int
	blobLimit       int
	blobMaxAttempts int
	runtimes        []upstreamRuntime
	next            int
	nextBlob        int
	limiters        *collectionmapping.Map[string, chan struct{}]
	health          *EndpointHealthTracker
	scheduler       *layerScheduler
	probeConfig     ProbeConfig
}

type upstreamRuntime struct {
	config Config
	client clienthttp.Client
	err    error
}

func newUpstreamPool(cfg Config, logger *slog.Logger) *upstreamPool {
	policy := normalizeMirrorPolicy(cfg.MirrorPolicy)
	pool := &upstreamPool{
		alias:           cfg.Alias,
		policy:          policy,
		blobPolicy:      normalizeBlobMirrorPolicy(cfg.Blob.MirrorPolicy, policy),
		blobTopN:        cfg.Blob.TopN,
		blobLimit:       cfg.Blob.MaxConcurrencyPerEndpoint,
		blobMaxAttempts: cfg.Blob.MaxConcurrentAttempts,
		probeConfig:     cfg.Probe,
		health: NewEndpointHealthTracker(EndpointHealthOptions{
			Cooldown: cfg.Probe.Cooldown,
		}),
		scheduler: newLayerScheduler(EndpointHealthOptions{
			Cooldown: cfg.Probe.Cooldown,
		}),
	}
	registries := endpointRegistries(cfg)
	runtimes := collectionlist.NewListWithCapacity[upstreamRuntime](len(registries))
	collectionlist.NewList(registries...).Range(func(_ int, registry string) bool {
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
		runtimes.Add(runtime)
		return true
	})
	pool.runtimes = runtimes.Values()
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
	collectionlist.NewList(cfg.Mirrors...).Range(func(_ int, registry string) bool {
		registry = strings.TrimRight(strings.TrimSpace(registry), "/")
		if registry == "" {
			return true
		}
		registries.Add(registry)
		return true
	})
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

type runtimeSelection struct {
	runtimes []upstreamRuntime
	release  func()
}

func newRuntimeSelection(runtimes []upstreamRuntime, release func()) runtimeSelection {
	if release == nil {
		release = func() {}
	}
	return runtimeSelection{runtimes: runtimes, release: release}
}

func (s runtimeSelection) Release() {
	if s.release != nil {
		s.release()
	}
}

func (p *upstreamPool) selectRuntimes(operation, repository, digest string) runtimeSelection {
	if p == nil {
		return newRuntimeSelection(nil, nil)
	}
	if operation == operationBlob {
		return p.selectBlobRuntimes(repository, digest)
	}
	return newRuntimeSelection(p.runtimesForPolicy(p.policy, false), nil)
}

func (p *upstreamPool) selectBlobRuntimes(repository, digest string) runtimeSelection {
	switch p.blobPolicy {
	case mirrorPolicyRoundRobin:
		return newRuntimeSelection(p.runtimesForPolicy(mirrorPolicyRoundRobin, true), nil)
	case mirrorPolicyLatency:
		return p.selectLatencyBlobRuntimes(repository, digest)
	default:
		return newRuntimeSelection(p.runtimesForPolicy(mirrorPolicyOrdered, true), nil)
	}
}

func (p *upstreamPool) runtimesForPolicy(policy string, blob bool) []upstreamRuntime {
	if len(p.runtimes) <= 1 || policy != mirrorPolicyRoundRobin {
		return p.runtimes
	}

	start := p.nextOffset(len(p.runtimes), blob)
	return collectionlist.MapList(collectionlist.NewList(p.runtimes...), func(i int, _ upstreamRuntime) upstreamRuntime {
		return p.runtimes[(start+i)%len(p.runtimes)]
	}).Values()
}

func (p *upstreamPool) selectLatencyBlobRuntimes(repository, digest string) runtimeSelection {
	if len(p.runtimes) <= 1 {
		return newRuntimeSelection(p.runtimes, nil)
	}
	now := time.Now()
	candidates := p.health.rankRuntimeCandidates(p.runtimes, repository, now)
	return p.scheduler.schedule(digest, candidates, p.blobTopN, p.blobAttemptConcurrency(), now)
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
	if p == nil || operation != operationBlob {
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
		p.limiters = collectionmapping.NewMap[string, chan struct{}]()
	}
	limiter, _ := p.limiters.Get(registry)
	if limiter == nil {
		limiter = make(chan struct{}, p.blobLimit)
		p.limiters.Set(registry, limiter)
	}
	return limiter
}

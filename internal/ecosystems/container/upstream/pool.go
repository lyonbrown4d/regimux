package upstream

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	clienthttp "github.com/arcgolabs/clientx/http"
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	collectionset "github.com/arcgolabs/collectionx/set"
	"golang.org/x/sync/semaphore"
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
	logger          *slog.Logger
	runtimes        *collectionlist.List[upstreamRuntime]
	next            int
	nextBlob        int
	limiters        *collectionmapping.ConcurrentMap[string, *semaphore.Weighted]
	health          *EndpointHealthTracker
	scheduler       *layerScheduler
	probeConfig     ProbeConfig
}

type upstreamRuntime struct {
	config Config
	client clienthttp.Client
	err    error
}

func newUpstreamPool(cfg Config, logger *slog.Logger, runtimes *collectionlist.List[upstreamRuntime]) *upstreamPool {
	policy := normalizeMirrorPolicy(cfg.MirrorPolicy)
	if runtimes == nil {
		runtimes = collectionlist.NewList[upstreamRuntime]()
	}
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
		limiters: collectionmapping.NewConcurrentMap[string, *semaphore.Weighted](),
		logger:   logger,
	}
	pool.runtimes = runtimes
	if logger != nil {
		logger.Debug(
			"upstream pool initialized",
			"alias", cfg.Alias,
			"endpoint_count", pool.runtimes.Len(),
			"mirror_policy", pool.policy,
			"blob_mirror_policy", pool.blobPolicy,
			"blob_top_n", pool.blobTopN,
			"blob_max_concurrency_per_endpoint", pool.blobLimit,
			"blob_max_concurrent_attempts", pool.blobMaxAttempts,
			"probe_enabled", pool.probeConfig.Enabled,
			"probe_interval", pool.probeConfig.Interval,
			"probe_timeout", pool.probeConfig.Timeout,
			"probe_cooldown", pool.probeConfig.Cooldown,
			"upstream_http2_enabled", cfg.HTTP.HTTP2.Enabled,
		)
	}
	return pool
}

func endpointRegistries(cfg Config) *collectionlist.List[string] {
	registries := collectionlist.NewList(cfg.Mirrors...)
	registries.Add(cfg.Registry)
	seen := collectionset.NewSetWithCapacity[string](registries.Len())
	out := collectionlist.NewList[string]()
	registries.Range(func(_ int, registry string) bool {
		registry = strings.TrimRight(strings.TrimSpace(registry), "/")
		if registry == "" || seen.Contains(registry) {
			return true
		}
		seen.Add(registry)
		out.Add(registry)
		return true
	})
	return out
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
	runtimes *collectionlist.List[upstreamRuntime]
	release  func()
}

func newRuntimeSelection(runtimes *collectionlist.List[upstreamRuntime], release func()) runtimeSelection {
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
	now := time.Now()
	selected := p.selectHealthyRuntimes(p.runtimes, repository, now, operation)
	return newRuntimeSelection(p.runtimesForPolicy(p.policy, selected, false), nil)
}

func (p *upstreamPool) selectBlobRuntimes(repository, digest string) runtimeSelection {
	now := time.Now()
	switch p.blobPolicy {
	case mirrorPolicyRoundRobin:
		selected := p.selectHealthyRuntimes(p.runtimes, repository, now, operationBlob)
		return newRuntimeSelection(p.runtimesForPolicy(mirrorPolicyRoundRobin, selected, true), nil)
	case mirrorPolicyLatency:
		selected := p.selectHealthyRuntimes(p.runtimes, repository, now, operationBlob)
		return p.selectLatencyBlobRuntimes(repository, digest, selected, now)
	default:
		selected := p.selectHealthyRuntimes(p.runtimes, repository, now, operationBlob)
		return newRuntimeSelection(p.runtimesForPolicy(mirrorPolicyOrdered, selected, true), nil)
	}
}

func (p *upstreamPool) runtimesForPolicy(policy string, runtimes *collectionlist.List[upstreamRuntime], blob bool) *collectionlist.List[upstreamRuntime] {
	if runtimes == nil || runtimes.Len() <= 1 || policy != mirrorPolicyRoundRobin {
		return runtimes
	}

	start := p.nextOffset(runtimes.Len(), blob)
	count := runtimes.Len()
	ordered := collectionlist.NewListWithCapacity[upstreamRuntime](count)
	for i := range count {
		runtime, ok := runtimes.Get((start + i) % count)
		if !ok {
			continue
		}
		ordered.Add(runtime)
	}
	return ordered
}

func (p *upstreamPool) selectLatencyBlobRuntimes(repository, digest string, runtimes *collectionlist.List[upstreamRuntime], now time.Time) runtimeSelection {
	if runtimes == nil || runtimes.Len() <= 1 {
		return newRuntimeSelection(runtimes, nil)
	}
	candidates := p.health.rankRuntimeCandidates(runtimes, repository, now)
	filtered := p.selectHealthyRuntimeCandidates(candidates, repository, operationBlob)
	return p.scheduler.schedule(digest, filtered, p.blobTopN, p.blobAttemptConcurrency(), now)
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

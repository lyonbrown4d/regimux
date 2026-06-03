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
	logger          *slog.Logger
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

func newUpstreamPool(cfg Config, logger *slog.Logger, runtimes []upstreamRuntime) *upstreamPool {
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
		logger: logger,
	}
	pool.runtimes = collectionlist.NewList(runtimes...).Values()
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
			"upstream_http2_enabled", cfg.HTTP.HTTP2.Enabled,
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

func (p *upstreamPool) runtimesForPolicy(policy string, runtimes []upstreamRuntime, blob bool) []upstreamRuntime {
	if len(runtimes) <= 1 || policy != mirrorPolicyRoundRobin {
		return runtimes
	}

	start := p.nextOffset(len(runtimes), blob)
	return collectionlist.MapList(collectionlist.NewList(runtimes...), func(i int, _ upstreamRuntime) upstreamRuntime {
		return runtimes[(start+i)%len(runtimes)]
	}).Values()
}

func (p *upstreamPool) selectLatencyBlobRuntimes(repository, digest string, runtimes []upstreamRuntime, now time.Time) runtimeSelection {
	if len(runtimes) <= 1 {
		return newRuntimeSelection(runtimes, nil)
	}
	candidates := p.health.rankRuntimeCandidates(runtimes, repository, now)
	candidates = p.selectHealthyRuntimeCandidates(candidates, repository, operationBlob)
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

func (p *upstreamPool) selectHealthyRuntimes(runtimes []upstreamRuntime, repository string, now time.Time, operation string) []upstreamRuntime {
	if p == nil {
		return runtimes
	}

	runtimeCandidates := p.toUpstreamRuntimeCandidates(runtimes, repository, now)
	// Order candidates by health score first so healthier endpoints are preferred for ordered
	// and round-robin selection paths before failover sequencing.
	// keep original index as deterministic tie-breaker.
	if p.health != nil && len(runtimeCandidates) > 1 {
		runtimeCandidates = p.health.rankRuntimeCandidates(runtimes, repository, now)
	}
	selectedCandidates := p.selectHealthyRuntimeCandidates(runtimeCandidates, repository, operation)
	return candidatesToRuntimes(selectedCandidates)
}

func (p *upstreamPool) selectHealthyRuntimeCandidates(
	candidates []endpointRuntimeCandidate,
	repository string,
	operation string,
) []endpointRuntimeCandidate {
	if p == nil || p.health == nil || len(candidates) <= 1 {
		return candidates
	}

	filtered := p.filterUnhealthyEndpointCandidates(candidates)
	if len(filtered) == len(candidates) {
		return candidates
	}
	if len(filtered) == 0 {
		if p.logger != nil {
			p.logger.Debug(
				"no healthy upstream endpoint candidates available, using all candidates",
				"alias", p.alias,
				"operation", operation,
				"repository", repository,
				"candidate_endpoints", runtimeRegistries(candidatesToRuntimes(candidates)),
			)
		}
		return candidates
	}
	if p.logger != nil {
		p.logger.Debug(
			"skipping unhealthy upstream endpoint candidates",
			"alias", p.alias,
			"operation", operation,
			"repository", repository,
			"candidate_endpoints", runtimeRegistries(candidatesToRuntimes(candidates)),
			"skipped_endpoints", len(candidates)-len(filtered),
			"selected_endpoints", runtimeRegistries(candidatesToRuntimes(filtered)),
		)
	}
	return filtered
}

func (p *upstreamPool) toUpstreamRuntimeCandidates(runtimes []upstreamRuntime, repository string, now time.Time) []endpointRuntimeCandidate {
	if p == nil {
		return nil
	}
	candidates := make([]endpointRuntimeCandidate, 0, len(runtimes))
	for i := range runtimes {
		runtime := runtimes[i]
		candidates = append(candidates, endpointRuntimeCandidate{
			runtime: runtime,
			state:   p.health.runtimeSnapshot(runtime.config.Registry, repository, now),
			index:   i,
		})
	}
	return candidates
}

func (p *upstreamPool) filterUnhealthyEndpointCandidates(candidates []endpointRuntimeCandidate) []endpointRuntimeCandidate {
	healthy := make([]endpointRuntimeCandidate, 0, len(candidates))
	for i := range candidates {
		candidate := candidates[i]
		if candidate.state.InCooldown || candidate.state.InDegraded {
			continue
		}
		healthy = append(healthy, candidate)
	}
	return healthy
}

func candidatesToRuntimes(candidates []endpointRuntimeCandidate) []upstreamRuntime {
	return collectionlist.MapList(collectionlist.NewList(candidates...), func(_ int, item endpointRuntimeCandidate) upstreamRuntime {
		return item.runtime
	}).Values()
}

func runtimeRegistries(runtimes []upstreamRuntime) []string {
	return collectionlist.MapList(collectionlist.NewList(runtimes...), func(_ int, runtime upstreamRuntime) string {
		return runtime.config.Registry
	}).Values()
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

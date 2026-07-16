package upstream

import (
	"context"
	"log/slog"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"golang.org/x/sync/semaphore"
)

type healthSelection uint8

const (
	healthSelectionAll healthSelection = iota
	healthSelectionNone
	healthSelectionMixed
)

type healthSelectionLog struct {
	selection  healthSelection
	operation  string
	repository string
	candidates []string
	selected   []string
}

func (p *upstreamPool) selectHealthyRuntimes(
	ctx context.Context,
	runtimes *collectionlist.List[upstreamRuntime],
	repository string,
	now time.Time,
	operation string,
) *collectionlist.List[upstreamRuntime] {
	if p == nil || p.health == nil || runtimes == nil || runtimes.Len() <= 1 {
		return runtimes
	}
	if p.shouldRankHealthyRuntimes(operation) {
		candidates := p.health.rankRuntimeCandidates(runtimes, repository, now)
		selected := p.selectHealthyRuntimeCandidates(ctx, candidates, repository, operation)
		return candidatesToRuntimes(selected)
	}

	selected, selection := filterHealthyItems(runtimes, func(runtime upstreamRuntime) bool {
		state := p.health.runtimeSnapshot(runtime.config.Registry, repository, now)
		return endpointAvailable(state)
	})
	if p.healthSelectionDebugEnabled(ctx) {
		p.logHealthSelection(healthSelectionLog{
			selection:  selection,
			operation:  operation,
			repository: repository,
			candidates: runtimeRegistryValues(runtimes),
			selected:   runtimeRegistryValues(selected),
		})
	}
	return selected
}

func (p *upstreamPool) shouldRankHealthyRuntimes(operation string) bool {
	if p == nil {
		return false
	}
	if operation == operationBlob {
		return p.blobPolicy != mirrorPolicyOrdered
	}
	return p.policy != mirrorPolicyOrdered
}

func (p *upstreamPool) selectHealthyRuntimeCandidates(
	ctx context.Context,
	candidates *collectionlist.List[endpointRuntimeCandidate],
	repository string,
	operation string,
) *collectionlist.List[endpointRuntimeCandidate] {
	if p == nil || p.health == nil || candidates == nil || candidates.Len() <= 1 {
		return candidates
	}

	selected, selection := filterHealthyItems(candidates, func(candidate endpointRuntimeCandidate) bool {
		return endpointAvailable(candidate.state)
	})
	if p.healthSelectionDebugEnabled(ctx) {
		p.logHealthSelection(healthSelectionLog{
			selection:  selection,
			operation:  operation,
			repository: repository,
			candidates: candidateRegistryValues(candidates),
			selected:   candidateRegistryValues(selected),
		})
	}
	return selected
}

func filterHealthyItems[T any](
	items *collectionlist.List[T],
	healthy func(T) bool,
) (*collectionlist.List[T], healthSelection) {
	if items == nil || items.Len() == 0 {
		return items, healthSelectionAll
	}

	first, _ := items.Get(0)
	firstHealthy := healthy(first)
	transition := firstHealthTransition(items, firstHealthy, healthy)
	if transition >= 0 {
		return collectMixedHealthyItems(items, transition, firstHealthy, healthy), healthSelectionMixed
	}
	if firstHealthy {
		return items, healthSelectionAll
	}
	return items, healthSelectionNone
}

func firstHealthTransition[T any](
	items *collectionlist.List[T],
	firstHealthy bool,
	healthy func(T) bool,
) int {
	transition := -1
	items.Range(func(index int, item T) bool {
		if index == 0 || healthy(item) == firstHealthy {
			return true
		}
		transition = index
		return false
	})
	return transition
}

func collectMixedHealthyItems[T any](
	items *collectionlist.List[T],
	transition int,
	firstHealthy bool,
	healthy func(T) bool,
) *collectionlist.List[T] {
	selected := collectionlist.NewListWithCapacity[T](items.Len())
	if firstHealthy {
		for index := range transition {
			item, _ := items.Get(index)
			selected.Add(item)
		}
	}
	for index := transition; index < items.Len(); index++ {
		item, _ := items.Get(index)
		if healthy(item) {
			selected.Add(item)
		}
	}
	return selected
}
func endpointAvailable(state EndpointHealthSnapshot) bool {
	return !state.InCooldown && !state.InDegraded
}

func (p *upstreamPool) healthSelectionDebugEnabled(ctx context.Context) bool {
	return p != nil &&
		p.logger != nil &&
		p.logger.Enabled(ctx, slog.LevelDebug)
}

func (p *upstreamPool) logHealthSelection(entry healthSelectionLog) {
	switch entry.selection {
	case healthSelectionNone:
		p.logger.Debug(
			"no healthy upstream endpoint candidates available, using all candidates",
			"alias", p.alias,
			"operation", entry.operation,
			"repository", entry.repository,
			"candidate_endpoints", entry.candidates,
		)
	case healthSelectionMixed:
		p.logger.Debug(
			"skipping unhealthy upstream endpoint candidates",
			"alias", p.alias,
			"operation", entry.operation,
			"repository", entry.repository,
			"candidate_endpoints", entry.candidates,
			"skipped_endpoints", len(entry.candidates)-len(entry.selected),
			"selected_endpoints", entry.selected,
		)
	case healthSelectionAll:
		return
	}
}

func candidatesToRuntimes(
	candidates *collectionlist.List[endpointRuntimeCandidate],
) *collectionlist.List[upstreamRuntime] {
	if candidates == nil {
		return collectionlist.NewList[upstreamRuntime]()
	}
	runtimes := collectionlist.NewListWithCapacity[upstreamRuntime](candidates.Len())
	candidates.Range(func(_ int, candidate endpointRuntimeCandidate) bool {
		runtimes.Add(candidate.runtime)
		return true
	})
	return runtimes
}

func runtimeRegistries(
	runtimes *collectionlist.List[upstreamRuntime],
) *collectionlist.List[string] {
	if runtimes == nil {
		return collectionlist.NewList[string]()
	}
	registries := collectionlist.NewListWithCapacity[string](runtimes.Len())
	runtimes.Range(func(_ int, runtime upstreamRuntime) bool {
		registries.Add(runtime.config.Registry)
		return true
	})
	return registries
}
func runtimeRegistryValues(runtimes *collectionlist.List[upstreamRuntime]) []string {
	if runtimes == nil {
		return nil
	}
	registries := make([]string, 0, runtimes.Len())
	runtimes.Range(func(_ int, runtime upstreamRuntime) bool {
		registries = append(registries, runtime.config.Registry)
		return true
	})
	return registries
}

func candidateRegistryValues(candidates *collectionlist.List[endpointRuntimeCandidate]) []string {
	if candidates == nil {
		return nil
	}
	registries := make([]string, 0, candidates.Len())
	candidates.Range(func(_ int, candidate endpointRuntimeCandidate) bool {
		registries = append(registries, candidate.runtime.config.Registry)
		return true
	})
	return registries
}

func (p *upstreamPool) acquireRuntime(
	ctx context.Context,
	operation string,
	runtime upstreamRuntime,
) (func(), error) {
	if p == nil || operation != operationBlob {
		return func() {}, nil
	}

	releaseHealth := p.health.Acquire(runtime.config.Registry)
	limiter := p.limiter(runtime.config.Registry)
	if limiter == nil {
		return releaseHealth, nil
	}

	if err := limiter.Acquire(ctx, 1); err != nil {
		releaseHealth()
		return nil, distribution.ErrUpstream.WithDetail(err.Error())
	}
	return func() {
		limiter.Release(1)
		releaseHealth()
	}, nil
}

func (p *upstreamPool) limiter(registry string) *semaphore.Weighted {
	if p == nil || p.blobLimit <= 0 {
		return nil
	}
	registry = normalizeEndpointHealthRegistry(registry)
	if limiter, ok := p.limiters.Get(registry); ok {
		return limiter
	}
	limiter, _ := p.limiters.GetOrStore(
		registry,
		semaphore.NewWeighted(int64(p.blobLimit)),
	)
	return limiter
}

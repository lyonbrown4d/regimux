package upstream

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/lo"
	"golang.org/x/sync/semaphore"
)

func (p *upstreamPool) selectHealthyRuntimes(runtimes *collectionlist.List[upstreamRuntime], repository string, now time.Time, operation string) *collectionlist.List[upstreamRuntime] {
	if p == nil {
		return runtimes
	}

	runtimeCandidates := p.toUpstreamRuntimeCandidates(runtimes, repository, now)
	if p.health != nil && runtimeCandidates != nil && runtimeCandidates.Len() > 1 {
		runtimeCandidates = p.health.rankRuntimeCandidates(runtimes, repository, now)
	}
	selectedCandidates := p.selectHealthyRuntimeCandidates(runtimeCandidates, repository, operation)
	return candidatesToRuntimes(selectedCandidates)
}

func (p *upstreamPool) selectHealthyRuntimeCandidates(
	candidates *collectionlist.List[endpointRuntimeCandidate],
	repository string,
	operation string,
) *collectionlist.List[endpointRuntimeCandidate] {
	if p == nil || p.health == nil || candidates == nil || candidates.Len() <= 1 {
		return candidates
	}

	filtered := p.filterUnhealthyEndpointCandidates(candidates)
	if filtered.Len() == candidates.Len() {
		return candidates
	}
	if filtered.Len() == 0 {
		if p.logger != nil {
			p.logger.Debug(
				"no healthy upstream endpoint candidates available, using all candidates",
				"alias", p.alias,
				"operation", operation,
				"repository", repository,
				"candidate_endpoints", runtimeRegistries(candidatesToRuntimes(candidates)).Values(),
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
			"candidate_endpoints", runtimeRegistries(candidatesToRuntimes(candidates)).Values(),
			"skipped_endpoints", candidates.Len()-filtered.Len(),
			"selected_endpoints", runtimeRegistries(candidatesToRuntimes(filtered)).Values(),
		)
	}
	return filtered
}

func (p *upstreamPool) toUpstreamRuntimeCandidates(runtimes *collectionlist.List[upstreamRuntime], repository string, now time.Time) *collectionlist.List[endpointRuntimeCandidate] {
	if p == nil || runtimes == nil {
		return collectionlist.NewList[endpointRuntimeCandidate]()
	}
	return collectionlist.NewList(lo.Map(runtimes.Values(), func(runtime upstreamRuntime, i int) endpointRuntimeCandidate {
		return endpointRuntimeCandidate{
			runtime: runtime,
			state:   p.health.runtimeSnapshot(runtime.config.Registry, repository, now),
			index:   i,
		}
	})...)
}

func (p *upstreamPool) filterUnhealthyEndpointCandidates(candidates *collectionlist.List[endpointRuntimeCandidate]) *collectionlist.List[endpointRuntimeCandidate] {
	if candidates == nil {
		return collectionlist.NewList[endpointRuntimeCandidate]()
	}
	return collectionlist.NewList(lo.Filter(candidates.Values(), func(candidate endpointRuntimeCandidate, _ int) bool {
		return !candidate.state.InCooldown && !candidate.state.InDegraded
	})...)
}

func candidatesToRuntimes(candidates *collectionlist.List[endpointRuntimeCandidate]) *collectionlist.List[upstreamRuntime] {
	if candidates == nil {
		return collectionlist.NewList[upstreamRuntime]()
	}
	return collectionlist.NewList(lo.Map(candidates.Values(), func(item endpointRuntimeCandidate, _ int) upstreamRuntime {
		return item.runtime
	})...)
}

func runtimeRegistries(runtimes *collectionlist.List[upstreamRuntime]) *collectionlist.List[string] {
	if runtimes == nil {
		return collectionlist.NewList[string]()
	}
	return collectionlist.NewList(lo.Map(runtimes.Values(), func(runtime upstreamRuntime, _ int) string {
		return runtime.config.Registry
	})...)
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
	limiter, _ := p.limiters.GetOrStore(registry, semaphore.NewWeighted(int64(p.blobLimit)))
	return limiter
}

package upstream

import (
	"sync"
	"time"

	collectionbitset "github.com/arcgolabs/collectionx/bitset"
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/samber/lo"
)

const defaultLayerSchedulerRecentWindow = 2 * time.Second

type layerSchedulerOptions struct {
	InflightPenalty    time.Duration
	RecentPenalty      time.Duration
	RecentWindow       time.Duration
	SameDigestAffinity time.Duration
}

type layerScheduler struct {
	mu             sync.Mutex
	opts           layerSchedulerOptions
	inFlight       *collectionmapping.Map[string, int]
	digestInFlight *collectionmapping.Table[string, string, int]
	recent         *collectionmapping.Map[string, time.Time]
}

type layerSchedulerCandidate struct {
	runtime upstreamRuntime
	state   EndpointHealthSnapshot
	score   time.Duration
	index   int
}

func newLayerScheduler(opts EndpointHealthOptions) *layerScheduler {
	healthOpts := normalizeEndpointHealthOptions(opts)
	return &layerScheduler{
		opts: layerSchedulerOptions{
			InflightPenalty:    healthOpts.InflightPenalty,
			RecentPenalty:      healthOpts.InflightPenalty,
			RecentWindow:       defaultLayerSchedulerRecentWindow,
			SameDigestAffinity: healthOpts.InflightPenalty,
		},
	}
}

func (s *layerScheduler) schedule(
	digest string,
	candidates *collectionlist.List[endpointRuntimeCandidate],
	topN int,
	reserveCount int,
	now time.Time,
) runtimeSelection {
	if s == nil || candidates == nil || candidates.Len() <= 1 {
		return newRuntimeSelection(runtimeCandidates(candidates), nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.initLocked()
	s.pruneRecentLocked(now)
	scored := s.scoreCandidatesLocked(digest, candidates, now)
	sortTopNCandidates(scored, topN)
	release := s.reserveLocked(digest, scored, reserveCount, now)
	return newRuntimeSelection(scheduledRuntimes(scored), release)
}

func (s *layerScheduler) scoreCandidatesLocked(
	digest string,
	candidates *collectionlist.List[endpointRuntimeCandidate],
	now time.Time,
) *collectionlist.List[layerSchedulerCandidate] {
	if candidates == nil {
		return collectionlist.NewList[layerSchedulerCandidate]()
	}
	return collectionlist.NewList(lo.Map(candidates.Values(), func(candidate endpointRuntimeCandidate, i int) layerSchedulerCandidate {
		registry := candidate.runtime.config.Registry
		score := s.scoreLocked(digest, registry, candidate.state, now)
		return layerSchedulerCandidate{
			runtime: candidate.runtime,
			state:   candidate.state,
			score:   score,
			index:   i,
		}
	})...)
}

func (s *layerScheduler) scoreLocked(digest, registry string, state EndpointHealthSnapshot, now time.Time) time.Duration {
	registry = normalizeEndpointHealthRegistry(registry)
	inFlight, _ := s.inFlight.Get(registry)
	score := state.Score +
		time.Duration(inFlight)*s.opts.InflightPenalty +
		s.recentPenaltyLocked(registry, now)
	return discountDuration(score, s.sameDigestAffinityLocked(digest, registry))
}

func (s *layerScheduler) recentPenaltyLocked(registry string, now time.Time) time.Duration {
	assignedAt, ok := s.recent.Get(registry)
	if !ok || s.opts.RecentWindow <= 0 {
		return 0
	}
	age := now.Sub(assignedAt)
	age = max(age, 0)
	if age >= s.opts.RecentWindow {
		return 0
	}
	return s.opts.RecentPenalty
}

func (s *layerScheduler) sameDigestAffinityLocked(digest, registry string) time.Duration {
	if digest == "" {
		return 0
	}
	count, ok := s.digestInFlight.Get(digest, registry)
	if !ok || count <= 0 {
		return 0
	}
	return s.opts.SameDigestAffinity
}

func sortTopNCandidates(candidates *collectionlist.List[layerSchedulerCandidate], topN int) {
	if candidates == nil {
		return
	}
	if topN <= 0 || topN > candidates.Len() {
		topN = candidates.Len()
	}
	if topN == candidates.Len() {
		sortCandidates(candidates)
		return
	}

	original := candidates.Clone()
	ranked := candidates.Clone().Sort(layerSchedulerCandidateCompare)
	selected := collectionbitset.New()

	ranked.Range(func(index int, candidate layerSchedulerCandidate) bool {
		if index >= topN {
			return false
		}
		selected.Set(candidate.index)
		return true
	})

	reordered := collectionlist.NewListWithCapacity[layerSchedulerCandidate](candidates.Len())
	ranked.Range(func(index int, candidate layerSchedulerCandidate) bool {
		if index >= topN {
			return false
		}
		reordered.Add(candidate)
		return true
	})
	original.Range(func(_ int, candidate layerSchedulerCandidate) bool {
		if selected.Contains(candidate.index) {
			return true
		}
		reordered.Add(candidate)
		return true
	})

	candidates.Clear()
	candidates.Merge(reordered)
}

func sortCandidates(candidates *collectionlist.List[layerSchedulerCandidate]) {
	candidates.Sort(layerSchedulerCandidateCompare)
}

func layerSchedulerCandidateCompare(left, right layerSchedulerCandidate) int {
	if left.state.InCooldown != right.state.InCooldown {
		if left.state.InCooldown {
			return 1
		}
		return -1
	}
	if left.score != right.score {
		if left.score < right.score {
			return -1
		}
		return 1
	}
	if left.index == right.index {
		return 0
	}
	if left.index < right.index {
		return -1
	}
	return 1
}

func scheduledRuntimes(candidates *collectionlist.List[layerSchedulerCandidate]) *collectionlist.List[upstreamRuntime] {
	if candidates == nil {
		return collectionlist.NewList[upstreamRuntime]()
	}
	return collectionlist.NewList(lo.Map(candidates.Values(), func(candidate layerSchedulerCandidate, _ int) upstreamRuntime {
		return candidate.runtime
	})...)
}

func runtimeCandidates(candidates *collectionlist.List[endpointRuntimeCandidate]) *collectionlist.List[upstreamRuntime] {
	if candidates == nil {
		return collectionlist.NewList[upstreamRuntime]()
	}
	return collectionlist.NewList(lo.Map(candidates.Values(), func(candidate endpointRuntimeCandidate, _ int) upstreamRuntime {
		return candidate.runtime
	})...)
}

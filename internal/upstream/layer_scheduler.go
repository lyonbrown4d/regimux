package upstream

import (
	"sort"
	"sync"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
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
	candidates []endpointRuntimeCandidate,
	topN int,
	reserveCount int,
	now time.Time,
) runtimeSelection {
	if s == nil || len(candidates) <= 1 {
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

func (s *layerScheduler) scoreCandidatesLocked(digest string, candidates []endpointRuntimeCandidate, now time.Time) []layerSchedulerCandidate {
	return collectionlist.MapList(collectionlist.NewList(candidates...), func(i int, candidate endpointRuntimeCandidate) layerSchedulerCandidate {
		registry := candidate.runtime.config.Registry
		score := s.scoreLocked(digest, registry, candidate.state, now)
		return layerSchedulerCandidate{
			runtime: candidate.runtime,
			state:   candidate.state,
			score:   score,
			index:   i,
		}
	}).Values()
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

func sortTopNCandidates(candidates []layerSchedulerCandidate, topN int) {
	if topN <= 0 || topN > len(candidates) {
		topN = len(candidates)
	}
	sort.SliceStable(candidates[:topN], func(i, j int) bool {
		return layerSchedulerCandidateLess(candidates[i], candidates[j])
	})
}

func layerSchedulerCandidateLess(left, right layerSchedulerCandidate) bool {
	if left.state.InCooldown != right.state.InCooldown {
		return !left.state.InCooldown
	}
	if left.score != right.score {
		return left.score < right.score
	}
	return left.index < right.index
}

func scheduledRuntimes(candidates []layerSchedulerCandidate) []upstreamRuntime {
	return collectionlist.MapList(collectionlist.NewList(candidates...), func(_ int, candidate layerSchedulerCandidate) upstreamRuntime {
		return candidate.runtime
	}).Values()
}

func runtimeCandidates(candidates []endpointRuntimeCandidate) []upstreamRuntime {
	return collectionlist.MapList(collectionlist.NewList(candidates...), func(_ int, candidate endpointRuntimeCandidate) upstreamRuntime {
		return candidate.runtime
	}).Values()
}

func (s *layerScheduler) reserveLocked(
	digest string,
	candidates []layerSchedulerCandidate,
	reserveCount int,
	now time.Time,
) func() {
	if reserveCount <= 0 || len(candidates) == 0 {
		return nil
	}
	if reserveCount > len(candidates) {
		reserveCount = len(candidates)
	}

	registries := collectionlist.NewListWithCapacity[string](reserveCount)
	for i := range reserveCount {
		registry := normalizeEndpointHealthRegistry(candidates[i].runtime.config.Registry)
		inFlight, _ := s.inFlight.Get(registry)
		s.inFlight.Set(registry, inFlight+1)
		s.recent.Set(registry, now)
		s.reserveDigestLocked(digest, registry)
		registries.Add(registry)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			s.release(digest, registries)
		})
	}
}

func (s *layerScheduler) reserveDigestLocked(digest, registry string) {
	if digest == "" {
		return
	}
	inFlight, _ := s.digestInFlight.Get(digest, registry)
	s.digestInFlight.Put(digest, registry, inFlight+1)
}

func (s *layerScheduler) release(digest string, registries *collectionlist.List[string]) {
	s.mu.Lock()
	defer s.mu.Unlock()

	registries.Range(func(_ int, registry string) bool {
		inFlight, _ := s.inFlight.Get(registry)
		if inFlight <= 1 {
			s.inFlight.Delete(registry)
		} else {
			s.inFlight.Set(registry, inFlight-1)
		}
		s.releaseDigestLocked(digest, registry)
		return true
	})
}

func (s *layerScheduler) releaseDigestLocked(digest, registry string) {
	if digest == "" {
		return
	}
	inFlight, ok := s.digestInFlight.Get(digest, registry)
	if !ok {
		return
	}
	if inFlight <= 1 {
		s.digestInFlight.Delete(digest, registry)
		return
	}
	s.digestInFlight.Put(digest, registry, inFlight-1)
}

func (s *layerScheduler) pruneRecentLocked(now time.Time) {
	if s.opts.RecentWindow <= 0 {
		return
	}
	s.recent.Range(func(registry string, assignedAt time.Time) bool {
		if now.Sub(assignedAt) >= s.opts.RecentWindow {
			s.recent.Delete(registry)
		}
		return true
	})
}

func (s *layerScheduler) initLocked() {
	if s.inFlight == nil {
		s.inFlight = collectionmapping.NewMap[string, int]()
	}
	if s.digestInFlight == nil {
		s.digestInFlight = collectionmapping.NewTable[string, string, int]()
	}
	if s.recent == nil {
		s.recent = collectionmapping.NewMap[string, time.Time]()
	}
	if s.opts.InflightPenalty <= 0 {
		s.opts.InflightPenalty = defaultEndpointHealthInflightPenalty
	}
	if s.opts.RecentPenalty <= 0 {
		s.opts.RecentPenalty = s.opts.InflightPenalty
	}
	if s.opts.RecentWindow <= 0 {
		s.opts.RecentWindow = defaultLayerSchedulerRecentWindow
	}
	if s.opts.SameDigestAffinity <= 0 {
		s.opts.SameDigestAffinity = s.opts.InflightPenalty
	}
}

func discountDuration(value, discount time.Duration) time.Duration {
	if discount <= 0 {
		return value
	}
	if discount >= value {
		return 0
	}
	return value - discount
}

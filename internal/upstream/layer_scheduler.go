package upstream

import (
	"sort"
	"sync"
	"time"
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
	inFlight       map[string]int
	digestInFlight map[string]map[string]int
	recent         map[string]time.Time
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
	scored := make([]layerSchedulerCandidate, 0, len(candidates))
	for i := range candidates {
		registry := candidates[i].runtime.config.Registry
		score := s.scoreLocked(digest, registry, candidates[i].state, now)
		scored = append(scored, layerSchedulerCandidate{
			runtime: candidates[i].runtime,
			state:   candidates[i].state,
			score:   score,
			index:   i,
		})
	}
	return scored
}

func (s *layerScheduler) scoreLocked(digest, registry string, state EndpointHealthSnapshot, now time.Time) time.Duration {
	registry = normalizeEndpointHealthRegistry(registry)
	score := state.Score +
		time.Duration(s.inFlight[registry])*s.opts.InflightPenalty +
		s.recentPenaltyLocked(registry, now)
	return discountDuration(score, s.sameDigestAffinityLocked(digest, registry))
}

func (s *layerScheduler) recentPenaltyLocked(registry string, now time.Time) time.Duration {
	assignedAt, ok := s.recent[registry]
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
	byRegistry := s.digestInFlight[digest]
	if byRegistry == nil || byRegistry[registry] <= 0 {
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
	runtimes := make([]upstreamRuntime, 0, len(candidates))
	for i := range candidates {
		runtimes = append(runtimes, candidates[i].runtime)
	}
	return runtimes
}

func runtimeCandidates(candidates []endpointRuntimeCandidate) []upstreamRuntime {
	runtimes := make([]upstreamRuntime, 0, len(candidates))
	for i := range candidates {
		runtimes = append(runtimes, candidates[i].runtime)
	}
	return runtimes
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

	registries := make([]string, 0, reserveCount)
	for i := range reserveCount {
		registry := normalizeEndpointHealthRegistry(candidates[i].runtime.config.Registry)
		s.inFlight[registry]++
		s.recent[registry] = now
		s.reserveDigestLocked(digest, registry)
		registries = append(registries, registry)
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
	byRegistry := s.digestInFlight[digest]
	if byRegistry == nil {
		byRegistry = make(map[string]int)
		s.digestInFlight[digest] = byRegistry
	}
	byRegistry[registry]++
}

func (s *layerScheduler) release(digest string, registries []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range registries {
		registry := registries[i]
		s.inFlight[registry]--
		if s.inFlight[registry] <= 0 {
			delete(s.inFlight, registry)
		}
		s.releaseDigestLocked(digest, registry)
	}
}

func (s *layerScheduler) releaseDigestLocked(digest, registry string) {
	if digest == "" {
		return
	}
	byRegistry := s.digestInFlight[digest]
	if byRegistry == nil {
		return
	}
	byRegistry[registry]--
	if byRegistry[registry] <= 0 {
		delete(byRegistry, registry)
	}
	if len(byRegistry) == 0 {
		delete(s.digestInFlight, digest)
	}
}

func (s *layerScheduler) pruneRecentLocked(now time.Time) {
	if s.opts.RecentWindow <= 0 {
		return
	}
	for registry, assignedAt := range s.recent {
		if now.Sub(assignedAt) >= s.opts.RecentWindow {
			delete(s.recent, registry)
		}
	}
}

func (s *layerScheduler) initLocked() {
	if s.inFlight == nil {
		s.inFlight = make(map[string]int)
	}
	if s.digestInFlight == nil {
		s.digestInFlight = make(map[string]map[string]int)
	}
	if s.recent == nil {
		s.recent = make(map[string]time.Time)
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

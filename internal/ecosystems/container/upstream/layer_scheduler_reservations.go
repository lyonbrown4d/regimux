package upstream

import (
	"sync"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
)

func (s *layerScheduler) reserveLocked(
	digest string,
	candidates *collectionlist.List[layerSchedulerCandidate],
	reserveCount int,
	now time.Time,
) func() {
	if candidates == nil || reserveCount <= 0 || candidates.Len() == 0 {
		return nil
	}
	if reserveCount > candidates.Len() {
		reserveCount = candidates.Len()
	}

	registries := collectionlist.NewListWithCapacity[string](reserveCount)
	for i := range reserveCount {
		candidate, ok := candidates.Get(i)
		if !ok {
			continue
		}
		registry := normalizeEndpointHealthRegistry(candidate.runtime.config.Registry)
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

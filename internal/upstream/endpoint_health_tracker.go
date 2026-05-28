package upstream

import (
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
)

func (t *EndpointHealthTracker) RecordProbeSuccess(registry string, latency time.Duration, now time.Time) EndpointHealthSnapshot {
	if latency < 0 {
		latency = 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateLocked(registry, "")
	opts := t.optionsLocked()
	if state.latencySamples == 0 {
		state.latencyEWMA = latency
	} else {
		state.latencyEWMA = time.Duration(float64(state.latencyEWMA)*(1-opts.Alpha) + float64(latency)*opts.Alpha)
	}
	state.latencySamples++
	state.consecutiveFailures = 0
	state.cooldownUntil = time.Time{}
	state.lastSuccessAt = now
	state.lastProbeAt = now
	return t.snapshotLocked(state, now)
}

func (t *EndpointHealthTracker) RecordProbeFailure(registry string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateLocked(registry, "")
	t.recordFailureLocked(state, "", now)
	state.lastProbeAt = now
	return t.snapshotLocked(state, now)
}

func (t *EndpointHealthTracker) RecordRequestSuccess(registry, repository string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	endpoint := t.stateLocked(registry, "")
	t.recordSuccessLocked(endpoint, now)
	if repository == "" {
		return t.snapshotLocked(endpoint, now)
	}
	repo := t.stateLocked(registry, repository)
	t.recordSuccessLocked(repo, now)
	return t.combinedSnapshotLocked(endpoint, repo, now)
}

func (t *EndpointHealthTracker) RecordRequestFailure(registry, repository string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	endpoint := t.stateLocked(registry, "")
	t.recordFailureLocked(endpoint, "", now)
	if repository == "" {
		return t.snapshotLocked(endpoint, now)
	}
	repo := t.stateLocked(registry, repository)
	t.recordFailureLocked(repo, repository, now)
	return t.combinedSnapshotLocked(endpoint, repo, now)
}

func (t *EndpointHealthTracker) RecordContentInconsistent(registry, repository string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	endpoint := t.stateLocked(registry, "")
	t.recordContentMismatchLocked(endpoint, now)
	if repository == "" {
		return t.snapshotLocked(endpoint, now)
	}
	repo := t.stateLocked(registry, repository)
	t.recordContentMismatchLocked(repo, now)
	return t.combinedSnapshotLocked(endpoint, repo, now)
}

func (t *EndpointHealthTracker) Acquire(registry string) func() {
	t.mu.Lock()
	state := t.stateLocked(registry, "")
	state.inflight++
	t.mu.Unlock()

	return func() {
		t.Release(registry)
	}
}

func (t *EndpointHealthTracker) Release(registry string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateLocked(registry, "")
	if state.inflight > 0 {
		state.inflight--
	}
}

func (t *EndpointHealthTracker) Snapshot(registry string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	registry = normalizeEndpointHealthRegistry(registry)
	state, _ := t.states.Get(endpointHealthStateKey(registry, ""))
	if state == nil {
		return t.snapshotLocked(&endpointHealthState{registry: registry}, now)
	}
	return t.snapshotLocked(state, now)
}

func (t *EndpointHealthTracker) RestoreSnapshot(snapshot EndpointHealthSnapshot) {
	if snapshot.Registry == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateLocked(snapshot.Registry, snapshot.Repository)
	state.latencyEWMA = snapshot.LatencyEWMA
	state.latencySamples = snapshot.LatencySamples
	state.consecutiveFailures = snapshot.ConsecutiveFailures
	state.cooldownUntil = snapshot.CooldownUntil
	state.degradedUntil = snapshot.DegradedUntil
	state.lastSuccessAt = snapshot.LastSuccessAt
	state.lastFailureAt = snapshot.LastFailureAt
	state.lastProbeAt = snapshot.LastProbeAt
	state.successCount = snapshot.SuccessCount
	state.failureCount = snapshot.FailureCount
	state.contentMismatchCount = snapshot.ContentMismatchCount
}

func (t *EndpointHealthTracker) RankEndpointCandidates(registries []string, now time.Time) []EndpointHealthCandidate {
	ranked := collectionlist.MapList(collectionlist.NewList(registries...), func(index int, registry string) endpointHealthCandidateRank {
		state := t.Snapshot(registry, now)
		return endpointHealthCandidateRank{
			candidate: EndpointHealthCandidate{Registry: state.Registry, State: state},
			index:     index,
		}
	}).Sort(compareEndpointHealthCandidateRank)
	return collectionlist.MapList(ranked, func(_ int, item endpointHealthCandidateRank) EndpointHealthCandidate {
		return item.candidate
	}).Values()
}

func (t *EndpointHealthTracker) rankRuntimeCandidates(runtimes []upstreamRuntime, repository string, now time.Time) []endpointRuntimeCandidate {
	return collectionlist.MapList(collectionlist.NewList(runtimes...), func(index int, runtime upstreamRuntime) endpointRuntimeCandidate {
		return endpointRuntimeCandidate{
			runtime: runtime,
			state:   t.runtimeSnapshot(runtime.config.Registry, repository, now),
			index:   index,
		}
	}).Sort(compareEndpointRuntimeCandidate).Values()
}

func (t *EndpointHealthTracker) runtimeSnapshot(registry, repository string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	endpoint := t.stateLocked(registry, "")
	if repository == "" {
		return t.snapshotLocked(endpoint, now)
	}
	repo, ok := t.states.Get(endpointHealthStateKey(registry, repository))
	if !ok || repo == nil {
		return t.snapshotLocked(endpoint, now)
	}
	return t.combinedSnapshotLocked(endpoint, repo, now)
}

func (t *EndpointHealthTracker) stateLocked(registry, repository string) *endpointHealthState {
	if t.states == nil {
		t.states = collectionmapping.NewMap[string, *endpointHealthState]()
	}
	t.opts = normalizeEndpointHealthOptions(t.opts)

	registry = normalizeEndpointHealthRegistry(registry)
	key := endpointHealthStateKey(registry, repository)
	state, _ := t.states.Get(key)
	if state == nil {
		state = &endpointHealthState{registry: registry, repository: repository}
		t.states.Set(key, state)
	}
	return state
}

func (t *EndpointHealthTracker) optionsLocked() EndpointHealthOptions {
	t.opts = normalizeEndpointHealthOptions(t.opts)
	return t.opts
}

package upstream

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultEndpointHealthAlpha           = 0.2
	defaultEndpointHealthFailurePenalty  = 500 * time.Millisecond
	defaultEndpointHealthInflightPenalty = 50 * time.Millisecond
	defaultEndpointHealthCooldown        = 2 * time.Minute
	defaultEndpointHealthUnknownLatency  = time.Second
)

type EndpointHealthOptions struct {
	Alpha           float64
	FailurePenalty  time.Duration
	InflightPenalty time.Duration
	Cooldown        time.Duration
	UnknownLatency  time.Duration
}

type EndpointHealthTracker struct {
	mu     sync.Mutex
	opts   EndpointHealthOptions
	states map[string]*endpointHealthState
}

type endpointHealthState struct {
	registry            string
	latencyEWMA         time.Duration
	latencySamples      int
	consecutiveFailures int
	cooldownUntil       time.Time
	inflight            int
	lastSuccessAt       time.Time
	lastFailureAt       time.Time
}

type EndpointHealthSnapshot struct {
	Registry            string
	LatencyEWMA         time.Duration
	HasLatency          bool
	ConsecutiveFailures int
	CooldownUntil       time.Time
	Inflight            int
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	Score               time.Duration
	InCooldown          bool
}

type EndpointHealthCandidate struct {
	Registry string
	State    EndpointHealthSnapshot
}

type endpointRuntimeCandidate struct {
	runtime upstreamRuntime
	state   EndpointHealthSnapshot
}

func NewEndpointHealthTracker(opts EndpointHealthOptions) *EndpointHealthTracker {
	return &EndpointHealthTracker{opts: normalizeEndpointHealthOptions(opts)}
}

func (t *EndpointHealthTracker) RecordProbeSuccess(registry string, latency time.Duration, now time.Time) EndpointHealthSnapshot {
	if latency < 0 {
		latency = 0
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateLocked(registry)
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
	return t.snapshotLocked(state, now)
}

func (t *EndpointHealthTracker) RecordProbeFailure(registry string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateLocked(registry)
	opts := t.optionsLocked()
	state.consecutiveFailures++
	state.cooldownUntil = now.Add(opts.Cooldown)
	state.lastFailureAt = now
	return t.snapshotLocked(state, now)
}

func (t *EndpointHealthTracker) Acquire(registry string) func() {
	t.mu.Lock()
	state := t.stateLocked(registry)
	state.inflight++
	t.mu.Unlock()

	return func() {
		t.Release(registry)
	}
}

func (t *EndpointHealthTracker) Release(registry string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.stateLocked(registry)
	if state.inflight > 0 {
		state.inflight--
	}
}

func (t *EndpointHealthTracker) Snapshot(registry string, now time.Time) EndpointHealthSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	registry = normalizeEndpointHealthRegistry(registry)
	state := t.states[registry]
	if state == nil {
		return t.snapshotLocked(&endpointHealthState{registry: registry}, now)
	}
	return t.snapshotLocked(state, now)
}

func (t *EndpointHealthTracker) RankEndpointCandidates(registries []string, now time.Time) []EndpointHealthCandidate {
	candidates := make([]EndpointHealthCandidate, 0, len(registries))
	for _, registry := range registries {
		state := t.Snapshot(registry, now)
		candidates = append(candidates, EndpointHealthCandidate{
			Registry: state.Registry,
			State:    state,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return endpointStateLess(candidates[i].State, candidates[j].State)
	})
	return candidates
}

func (t *EndpointHealthTracker) rankRuntimeCandidates(runtimes []upstreamRuntime, now time.Time) []endpointRuntimeCandidate {
	candidates := make([]endpointRuntimeCandidate, 0, len(runtimes))
	for _, runtime := range runtimes {
		candidates = append(candidates, endpointRuntimeCandidate{
			runtime: runtime,
			state:   t.Snapshot(runtime.config.Registry, now),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return endpointStateLess(candidates[i].state, candidates[j].state)
	})
	return candidates
}

func (t *EndpointHealthTracker) rankRuntimes(runtimes []upstreamRuntime, now time.Time) []upstreamRuntime {
	candidates := t.rankRuntimeCandidates(runtimes, now)
	out := make([]upstreamRuntime, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.runtime)
	}
	return out
}

func (t *EndpointHealthTracker) stateLocked(registry string) *endpointHealthState {
	if t.states == nil {
		t.states = make(map[string]*endpointHealthState)
	}
	t.opts = normalizeEndpointHealthOptions(t.opts)

	registry = normalizeEndpointHealthRegistry(registry)
	state := t.states[registry]
	if state == nil {
		state = &endpointHealthState{registry: registry}
		t.states[registry] = state
	}
	return state
}

func (t *EndpointHealthTracker) optionsLocked() EndpointHealthOptions {
	t.opts = normalizeEndpointHealthOptions(t.opts)
	return t.opts
}

func (t *EndpointHealthTracker) snapshotLocked(state *endpointHealthState, now time.Time) EndpointHealthSnapshot {
	opts := t.optionsLocked()
	hasLatency := state.latencySamples > 0
	scoreLatency := opts.UnknownLatency
	if hasLatency {
		scoreLatency = state.latencyEWMA
	}

	score := scoreLatency +
		time.Duration(state.consecutiveFailures)*opts.FailurePenalty +
		time.Duration(state.inflight)*opts.InflightPenalty

	return EndpointHealthSnapshot{
		Registry:            state.registry,
		LatencyEWMA:         state.latencyEWMA,
		HasLatency:          hasLatency,
		ConsecutiveFailures: state.consecutiveFailures,
		CooldownUntil:       state.cooldownUntil,
		Inflight:            state.inflight,
		LastSuccessAt:       state.lastSuccessAt,
		LastFailureAt:       state.lastFailureAt,
		Score:               score,
		InCooldown:          now.Before(state.cooldownUntil),
	}
}

func endpointStateLess(left, right EndpointHealthSnapshot) bool {
	if left.InCooldown != right.InCooldown {
		return !left.InCooldown
	}
	if left.Score != right.Score {
		return left.Score < right.Score
	}
	return false
}

func normalizeEndpointHealthOptions(opts EndpointHealthOptions) EndpointHealthOptions {
	if opts.Alpha <= 0 || opts.Alpha > 1 {
		opts.Alpha = defaultEndpointHealthAlpha
	}
	if opts.FailurePenalty <= 0 {
		opts.FailurePenalty = defaultEndpointHealthFailurePenalty
	}
	if opts.InflightPenalty <= 0 {
		opts.InflightPenalty = defaultEndpointHealthInflightPenalty
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = defaultEndpointHealthCooldown
	}
	if opts.UnknownLatency <= 0 {
		opts.UnknownLatency = defaultEndpointHealthUnknownLatency
	}
	return opts
}

func normalizeEndpointHealthRegistry(registry string) string {
	return strings.TrimRight(strings.TrimSpace(registry), "/")
}

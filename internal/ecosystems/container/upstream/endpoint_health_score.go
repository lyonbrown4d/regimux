package upstream

import (
	"cmp"
	"time"
)

func (t *EndpointHealthTracker) recordSuccessLocked(state *endpointHealthState, now time.Time) {
	state.successCount++
	state.consecutiveFailures = 0
	state.cooldownUntil = time.Time{}
	state.lastSuccessAt = now
}

func (t *EndpointHealthTracker) recordFailureLocked(state *endpointHealthState, repository string, now time.Time) {
	opts := t.optionsLocked()
	state.failureCount++
	state.consecutiveFailures++
	if state.consecutiveFailures >= opts.FailureThreshold {
		state.cooldownUntil = now.Add(opts.Cooldown)
	}
	state.lastFailureAt = now
	if repository != "" {
		state.repository = repository
	}
}

func (t *EndpointHealthTracker) recordContentMismatchLocked(state *endpointHealthState, now time.Time) {
	opts := t.optionsLocked()
	state.contentMismatchCount++
	state.failureCount++
	state.degradedUntil = now.Add(opts.ContentMismatchCooldown)
	state.lastFailureAt = now
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
	if now.Before(state.degradedUntil) {
		score += opts.FailurePenalty
	}
	successRate, hasSuccessRate := successRate(state.successCount, state.failureCount)
	return EndpointHealthSnapshot{
		Registry:             state.registry,
		Repository:           state.repository,
		LatencyEWMA:          state.latencyEWMA,
		LatencySamples:       state.latencySamples,
		HasLatency:           hasLatency,
		ConsecutiveFailures:  state.consecutiveFailures,
		CooldownUntil:        state.cooldownUntil,
		DegradedUntil:        state.degradedUntil,
		Inflight:             state.inflight,
		LastSuccessAt:        state.lastSuccessAt,
		LastFailureAt:        state.lastFailureAt,
		LastProbeAt:          state.lastProbeAt,
		SuccessCount:         state.successCount,
		FailureCount:         state.failureCount,
		ContentMismatchCount: state.contentMismatchCount,
		HasSuccessRate:       hasSuccessRate,
		SuccessRate:          successRate,
		Score:                score,
		InCooldown:           now.Before(state.cooldownUntil),
		InDegraded:           now.Before(state.degradedUntil),
	}
}

func (t *EndpointHealthTracker) combinedSnapshotLocked(endpoint, repo *endpointHealthState, now time.Time) EndpointHealthSnapshot {
	snapshot := t.snapshotLocked(endpoint, now)
	repoSnapshot := t.snapshotLocked(repo, now)
	snapshot.Repository = repo.repository
	snapshot.SuccessCount = repo.successCount
	snapshot.FailureCount = repo.failureCount
	snapshot.ContentMismatchCount = repo.contentMismatchCount
	snapshot.HasSuccessRate = repoSnapshot.HasSuccessRate
	snapshot.SuccessRate = repoSnapshot.SuccessRate
	snapshot.DegradedUntil = maxTime(snapshot.DegradedUntil, repo.degradedUntil)
	snapshot.LastSuccessAt = maxTime(snapshot.LastSuccessAt, repo.lastSuccessAt)
	snapshot.LastFailureAt = maxTime(snapshot.LastFailureAt, repo.lastFailureAt)
	snapshot.InDegraded = now.Before(snapshot.DegradedUntil)
	if repo.failureCount > 0 {
		snapshot.Score += time.Duration(repo.failureCount) * t.optionsLocked().FailurePenalty
	}
	if snapshot.InDegraded {
		snapshot.Score += t.optionsLocked().FailurePenalty
	}
	return snapshot
}

func successRate(successes, failures int64) (float64, bool) {
	total := successes + failures
	if total <= 0 {
		return 0, false
	}
	return float64(successes) / float64(total), true
}

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func compareEndpointHealthCandidateRank(left, right endpointHealthCandidateRank) int {
	if state := compareEndpointState(left.candidate.State, right.candidate.State); state != 0 {
		return state
	}
	return cmp.Compare(left.index, right.index)
}

func compareEndpointRuntimeCandidate(left, right endpointRuntimeCandidate) int {
	if state := compareEndpointState(left.state, right.state); state != 0 {
		return state
	}
	return cmp.Compare(left.index, right.index)
}

func compareEndpointState(left, right EndpointHealthSnapshot) int {
	if left.InCooldown != right.InCooldown {
		if left.InCooldown {
			return 1
		}
		return -1
	}
	if left.InDegraded != right.InDegraded {
		if left.InDegraded {
			return 1
		}
		return -1
	}
	return cmp.Compare(left.Score, right.Score)
}

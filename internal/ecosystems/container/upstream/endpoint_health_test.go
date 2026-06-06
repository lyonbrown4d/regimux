package upstream_test

import (
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
)

func TestEndpointHealthRanksLowerLatencyFirst(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	tracker := upstream.NewEndpointHealthTracker(upstream.EndpointHealthOptions{})
	tracker.RecordProbeSuccess("https://slow.example", 200*time.Millisecond, now)
	tracker.RecordProbeSuccess("https://fast.example", 20*time.Millisecond, now)

	ranked := tracker.RankEndpointCandidates(collectionlist.NewList(
		"https://unknown.example",
		"https://slow.example",
		"https://fast.example",
	), now)

	requireRegistryOrder(t, ranked, []string{
		"https://fast.example",
		"https://slow.example",
		"https://unknown.example",
	})
}

func TestEndpointHealthFailureEntersCooldown(t *testing.T) {
	t.Parallel()

	now := time.Unix(200, 0)
	tracker := upstream.NewEndpointHealthTracker(upstream.EndpointHealthOptions{
		Cooldown:         time.Minute,
		FailurePenalty:   50 * time.Millisecond,
		FailureThreshold: 1,
	})
	tracker.RecordProbeSuccess("https://failed.example", 5*time.Millisecond, now)
	tracker.RecordProbeSuccess("https://healthy.example", 100*time.Millisecond, now)
	failed := tracker.RecordProbeFailure("https://failed.example", now)

	if !failed.InCooldown {
		t.Fatal("failed endpoint is not in cooldown")
	}
	if failed.CooldownUntil != now.Add(time.Minute) {
		t.Fatalf("cooldown_until = %v, want %v", failed.CooldownUntil, now.Add(time.Minute))
	}
	if failed.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive failures = %d, want 1", failed.ConsecutiveFailures)
	}

	candidates := tracker.RankEndpointCandidates(collectionlist.NewList(
		"https://failed.example",
		"https://healthy.example",
	), now)
	candidateValues := candidates.Values()
	if candidateValues[0].Registry != "https://healthy.example" {
		t.Fatalf("first registry = %q, want healthy endpoint", candidateValues[0].Registry)
	}
	if !candidateValues[1].State.InCooldown {
		t.Fatal("cooldown endpoint was not ranked as cooldown")
	}
}

func TestEndpointHealthInflightPenaltySpreadsSelection(t *testing.T) {
	t.Parallel()

	now := time.Unix(300, 0)
	tracker := upstream.NewEndpointHealthTracker(upstream.EndpointHealthOptions{
		InflightPenalty: 100 * time.Millisecond,
	})
	tracker.RecordProbeSuccess("https://fast.example", 20*time.Millisecond, now)
	tracker.RecordProbeSuccess("https://second.example", 60*time.Millisecond, now)

	release := tracker.Acquire("https://fast.example")
	candidates := tracker.RankEndpointCandidates(collectionlist.NewList(
		"https://fast.example",
		"https://second.example",
	), now)
	candidateValues := candidates.Values()
	if candidateValues[0].Registry != "https://second.example" {
		t.Fatalf("first registry with inflight penalty = %q, want second endpoint", candidateValues[0].Registry)
	}
	if candidateValues[1].State.Inflight != 1 {
		t.Fatalf("fast endpoint inflight = %d, want 1", candidateValues[1].State.Inflight)
	}

	release()
	candidates = tracker.RankEndpointCandidates(collectionlist.NewList(
		"https://fast.example",
		"https://second.example",
	), now)
	candidateValues = candidates.Values()
	if candidateValues[0].Registry != "https://fast.example" {
		t.Fatalf("first registry after release = %q, want fast endpoint", candidateValues[0].Registry)
	}
}

func TestEndpointHealthEWMADoesNotJumpOnSingleSample(t *testing.T) {
	t.Parallel()

	now := time.Unix(400, 0)
	tracker := upstream.NewEndpointHealthTracker(upstream.EndpointHealthOptions{
		Alpha: 0.2,
	})
	tracker.RecordProbeSuccess("https://mirror.example", 100*time.Millisecond, now)
	snapshot := tracker.RecordProbeSuccess("https://mirror.example", time.Second, now.Add(time.Second))

	want := 280 * time.Millisecond
	if snapshot.LatencyEWMA != want {
		t.Fatalf("latency EWMA = %v, want %v", snapshot.LatencyEWMA, want)
	}
	if snapshot.LatencyEWMA >= time.Second {
		t.Fatalf("latency EWMA jumped to latest sample: %v", snapshot.LatencyEWMA)
	}
}

func TestEndpointHealthCircuitBreakerRequiresRepeatedFailures(t *testing.T) {
	t.Parallel()

	now := time.Unix(500, 0)
	tracker := upstream.NewEndpointHealthTracker(upstream.EndpointHealthOptions{
		Cooldown:         time.Minute,
		FailureThreshold: 3,
	})

	first := tracker.RecordProbeFailure("https://mirror.example", now)
	second := tracker.RecordProbeFailure("https://mirror.example", now.Add(time.Second))
	third := tracker.RecordProbeFailure("https://mirror.example", now.Add(2*time.Second))

	if first.InCooldown || second.InCooldown {
		t.Fatalf("endpoint entered cooldown before threshold: first=%#v second=%#v", first, second)
	}
	if !third.InCooldown || third.CooldownUntil != now.Add(2*time.Second).Add(time.Minute) {
		t.Fatalf("unexpected circuit breaker snapshot: %#v", third)
	}
}

func TestEndpointHealthTracksRepositorySuccessRate(t *testing.T) {
	t.Parallel()

	now := time.Unix(600, 0)
	tracker := upstream.NewEndpointHealthTracker(upstream.EndpointHealthOptions{})
	tracker.RecordRequestSuccess("https://mirror.example", "library/nginx", now)
	snapshot := tracker.RecordRequestFailure("https://mirror.example", "library/nginx", now.Add(time.Second))

	if !snapshot.HasSuccessRate || snapshot.SuccessCount != 1 || snapshot.FailureCount != 1 {
		t.Fatalf("unexpected repository counters: %#v", snapshot)
	}
	if snapshot.SuccessRate != 0.5 {
		t.Fatalf("success rate = %v, want 0.5", snapshot.SuccessRate)
	}
	endpoint := tracker.Snapshot("https://mirror.example", now.Add(time.Second))
	if endpoint.SuccessCount != 1 || endpoint.FailureCount != 1 {
		t.Fatalf("endpoint counters were not updated: %#v", endpoint)
	}
}

func TestEndpointHealthContentMismatchDegradesEndpoint(t *testing.T) {
	t.Parallel()

	now := time.Unix(700, 0)
	tracker := upstream.NewEndpointHealthTracker(upstream.EndpointHealthOptions{
		ContentMismatchCooldown: time.Minute,
	})
	snapshot := tracker.RecordContentInconsistent("https://mirror.example", "library/nginx", now)

	if !snapshot.InDegraded || snapshot.DegradedUntil != now.Add(time.Minute) {
		t.Fatalf("endpoint not degraded after content mismatch: %#v", snapshot)
	}
	if snapshot.ContentMismatchCount != 1 || snapshot.FailureCount != 1 {
		t.Fatalf("content mismatch counters were not updated: %#v", snapshot)
	}
}

func requireRegistryOrder(t *testing.T, candidates *collectionlist.List[upstream.EndpointHealthCandidate], want []string) {
	t.Helper()
	values := candidates.Values()
	if len(values) != len(want) {
		t.Fatalf("candidate count = %d, want %d", len(values), len(want))
	}
	for i := range values {
		if values[i].Registry != want[i] {
			t.Fatalf("candidate[%d] = %q, want %q", i, values[i].Registry, want[i])
		}
	}
}

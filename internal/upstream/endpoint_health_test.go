package upstream

import (
	"testing"
	"time"
)

func TestEndpointHealthRanksLowerLatencyFirst(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	tracker := NewEndpointHealthTracker(EndpointHealthOptions{})
	tracker.RecordProbeSuccess("https://slow.example", 200*time.Millisecond, now)
	tracker.RecordProbeSuccess("https://fast.example", 20*time.Millisecond, now)

	runtimes := []upstreamRuntime{
		{config: Config{Registry: "https://unknown.example"}},
		{config: Config{Registry: "https://slow.example"}},
		{config: Config{Registry: "https://fast.example"}},
	}
	ranked := tracker.rankRuntimes(runtimes, now)

	requireRegistryOrder(t, ranked, []string{
		"https://fast.example",
		"https://slow.example",
		"https://unknown.example",
	})
}

func TestEndpointHealthFailureEntersCooldown(t *testing.T) {
	t.Parallel()

	now := time.Unix(200, 0)
	tracker := NewEndpointHealthTracker(EndpointHealthOptions{
		Cooldown:       time.Minute,
		FailurePenalty: 50 * time.Millisecond,
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

	candidates := tracker.RankEndpointCandidates([]string{
		"https://failed.example",
		"https://healthy.example",
	}, now)
	if candidates[0].Registry != "https://healthy.example" {
		t.Fatalf("first registry = %q, want healthy endpoint", candidates[0].Registry)
	}
	if !candidates[1].State.InCooldown {
		t.Fatal("cooldown endpoint was not ranked as cooldown")
	}
}

func TestEndpointHealthInflightPenaltySpreadsSelection(t *testing.T) {
	t.Parallel()

	now := time.Unix(300, 0)
	tracker := NewEndpointHealthTracker(EndpointHealthOptions{
		InflightPenalty: 100 * time.Millisecond,
	})
	tracker.RecordProbeSuccess("https://fast.example", 20*time.Millisecond, now)
	tracker.RecordProbeSuccess("https://second.example", 60*time.Millisecond, now)

	release := tracker.Acquire("https://fast.example")
	candidates := tracker.RankEndpointCandidates([]string{
		"https://fast.example",
		"https://second.example",
	}, now)
	if candidates[0].Registry != "https://second.example" {
		t.Fatalf("first registry with inflight penalty = %q, want second endpoint", candidates[0].Registry)
	}
	if candidates[1].State.Inflight != 1 {
		t.Fatalf("fast endpoint inflight = %d, want 1", candidates[1].State.Inflight)
	}

	release()
	candidates = tracker.RankEndpointCandidates([]string{
		"https://fast.example",
		"https://second.example",
	}, now)
	if candidates[0].Registry != "https://fast.example" {
		t.Fatalf("first registry after release = %q, want fast endpoint", candidates[0].Registry)
	}
}

func TestEndpointHealthEWMADoesNotJumpOnSingleSample(t *testing.T) {
	t.Parallel()

	now := time.Unix(400, 0)
	tracker := NewEndpointHealthTracker(EndpointHealthOptions{
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

func requireRegistryOrder(t *testing.T, runtimes []upstreamRuntime, want []string) {
	t.Helper()
	if len(runtimes) != len(want) {
		t.Fatalf("runtime count = %d, want %d", len(runtimes), len(want))
	}
	for i := range runtimes {
		if runtimes[i].config.Registry != want[i] {
			t.Fatalf("runtime[%d] = %q, want %q", i, runtimes[i].config.Registry, want[i])
		}
	}
}

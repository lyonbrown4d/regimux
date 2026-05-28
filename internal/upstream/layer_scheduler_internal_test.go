package upstream

import (
	"testing"
	"time"
)

func TestLayerSchedulerTopNSelectsLowScoreOutsidePrefix(t *testing.T) {
	t.Parallel()

	scheduler := newLayerScheduler(EndpointHealthOptions{})
	selection := scheduler.schedule("", []endpointRuntimeCandidate{
		schedulerCandidate("first", 100*time.Millisecond, 0),
		schedulerCandidate("second", 100*time.Millisecond, 1),
		schedulerCandidate("third", 10*time.Millisecond, 2),
	}, 2, 0, time.Now())

	got := runtimeRegistries(selection.runtimes)
	want := []string{"third", "first", "second"}
	if len(got) != len(want) {
		t.Fatalf("selected runtimes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected runtimes = %v, want %v", got, want)
		}
	}
}

func schedulerCandidate(registry string, score time.Duration, index int) endpointRuntimeCandidate {
	return endpointRuntimeCandidate{
		runtime: upstreamRuntime{config: Config{Registry: registry}},
		state:   EndpointHealthSnapshot{Score: score},
		index:   index,
	}
}

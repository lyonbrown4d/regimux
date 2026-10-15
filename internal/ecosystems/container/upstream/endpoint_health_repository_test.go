//nolint:testpackage // This verifies repository-scoped selection state directly.
package upstream

import (
	"testing"
	"time"
)

func TestRepositoryCoverageFailureOnlyCoolsDownThatRepository(t *testing.T) {
	now := time.Unix(1_000, 0)
	tracker := NewEndpointHealthTracker(EndpointHealthOptions{Cooldown: time.Minute})

	denied := tracker.RecordRepositoryRequestFailure("https://mirror.example", "timescale/timescaledb-ha", now)
	if !denied.InCooldown {
		t.Fatal("denied repository did not enter cooldown")
	}
	if denied.CooldownUntil != now.Add(time.Minute) {
		t.Fatalf("repository cooldown_until = %v, want %v", denied.CooldownUntil, now.Add(time.Minute))
	}

	endpoint := tracker.Snapshot("https://mirror.example", now)
	if endpoint.InCooldown {
		t.Fatal("repository coverage failure cooled down the whole endpoint")
	}
	if otherRepository := tracker.runtimeSnapshot("https://mirror.example", "library/nginx", now); otherRepository.InCooldown {
		t.Fatal("repository coverage failure cooled down an unrelated repository")
	}
	if sameRepository := tracker.runtimeSnapshot("https://mirror.example", "timescale/timescaledb-ha", now); !sameRepository.InCooldown {
		t.Fatal("repository-specific cooldown was not used during runtime selection")
	}
}

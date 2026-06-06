package config_test

import (
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

const testHCLScheduler = `
scheduler {
  manifest_refresh {
    enabled = true
    interval = "30m"
    distributed = true

    ecosystems {
      container {
        interval = "10m"
      }

      go {
        enabled = false
      }

      npm {
        enabled = true
        interval = "45m"
        distributed = false
      }
    }
  }
}
`

func assertLoadedHCLScheduler(t *testing.T, scheduler config.SchedulerConfig) {
	t.Helper()

	manifestRefresh := scheduler.ManifestRefresh
	assertRefreshConfig(t, "global", refreshConfigSnapshot{
		enabled:     manifestRefresh.Enabled,
		interval:    manifestRefresh.Interval,
		distributed: manifestRefresh.Distributed,
	}, refreshConfigSnapshot{enabled: true, interval: 30 * time.Minute, distributed: true})
	assertRefreshConfig(t, "container", refreshConfigSnapshotFromResolved(manifestRefresh.EffectiveFor("container")), refreshConfigSnapshot{
		enabled:     true,
		interval:    10 * time.Minute,
		distributed: true,
	})
	assertRefreshConfig(t, "go", refreshConfigSnapshotFromResolved(manifestRefresh.EffectiveFor("go")), refreshConfigSnapshot{
		enabled:     false,
		interval:    30 * time.Minute,
		distributed: true,
	})
	assertRefreshConfig(t, "npm", refreshConfigSnapshotFromResolved(manifestRefresh.EffectiveFor("npm")), refreshConfigSnapshot{
		enabled:     true,
		interval:    45 * time.Minute,
		distributed: false,
	})
}

type refreshConfigSnapshot struct {
	enabled     bool
	interval    time.Duration
	distributed bool
}

func refreshConfigSnapshotFromResolved(cfg config.SchedulerResolvedRefreshConfig) refreshConfigSnapshot {
	return refreshConfigSnapshot{
		enabled:     cfg.Enabled,
		interval:    cfg.Interval,
		distributed: cfg.Distributed,
	}
}

func assertRefreshConfig(t *testing.T, name string, got, want refreshConfigSnapshot) {
	t.Helper()

	if got != want {
		t.Fatalf("unexpected %s manifest refresh config: got %#v want %#v", name, got, want)
	}
}

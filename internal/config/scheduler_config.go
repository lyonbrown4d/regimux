package config

import (
	"strings"
	"time"
)

type SchedulerResolvedRefreshConfig struct {
	Enabled     bool
	Interval    time.Duration
	Distributed bool
}

func (c *Config) normalizeScheduler() {
	if c == nil {
		return
	}
	c.Scheduler.ManifestRefresh.Ecosystems = normalizeRefreshEcosystems(c.Scheduler.ManifestRefresh.Ecosystems)
}

func normalizeRefreshEcosystems(values map[string]SchedulerEcosystemRefreshConfig) map[string]SchedulerEcosystemRefreshConfig {
	if len(values) == 0 {
		return nil
	}
	normalized := make(map[string]SchedulerEcosystemRefreshConfig, len(values))
	for ecosystemName, cfg := range values {
		ecosystemName = strings.ToLower(strings.TrimSpace(ecosystemName))
		if ecosystemName == "" {
			continue
		}
		normalized[ecosystemName] = cfg
	}
	return normalized
}

func (cfg SchedulerManifestRefreshConfig) EffectiveFor(ecosystemName string) SchedulerResolvedRefreshConfig {
	resolved := SchedulerResolvedRefreshConfig{
		Enabled:     cfg.Enabled,
		Interval:    cfg.Interval,
		Distributed: cfg.Distributed,
	}
	override, ok := cfg.Ecosystems[strings.ToLower(strings.TrimSpace(ecosystemName))]
	if !ok {
		return resolved
	}
	if override.Enabled != nil {
		resolved.Enabled = *override.Enabled
	}
	if override.Interval > 0 {
		resolved.Interval = override.Interval
	}
	if override.Distributed != nil {
		resolved.Distributed = *override.Distributed
	}
	return resolved
}

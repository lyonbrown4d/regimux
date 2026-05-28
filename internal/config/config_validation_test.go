package config_test

import (
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

type invalidConfigCase struct {
	name   string
	mutate func(*config.Config)
}

func TestValidateUpstreamBlobAndProbeRejectsInvalidValues(t *testing.T) {
	for _, tt := range invalidBlobProbeCases() {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadDefaultConfig(t)
			tt.mutate(&cfg)
			if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
				t.Fatal("expected upstream blob/probe validation error")
			}
		})
	}
}

func TestValidateAuthRejectsInvalidEnabledConfig(t *testing.T) {
	for _, tt := range []invalidConfigCase{
		{name: "missing secret", mutate: func(cfg *config.Config) {
			cfg.Auth.Enabled = true
			cfg.Auth.Users = map[string]config.AuthUserConfig{
				"alice": {Password: "secret", Repositories: []string{"hub/*"}},
			}
		}},
		{name: "missing users", mutate: func(cfg *config.Config) {
			cfg.Auth.Enabled = true
			cfg.Auth.TokenSecret = "secret"
		}},
		{name: "missing password", mutate: func(cfg *config.Config) {
			cfg.Auth.Enabled = true
			cfg.Auth.TokenSecret = "secret"
			cfg.Auth.Users = map[string]config.AuthUserConfig{
				"alice": {Repositories: []string{"hub/*"}},
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := loadDefaultConfig(t)
			tt.mutate(&cfg)
			if err := cfg.NormalizeAndValidate(); err == nil {
				t.Fatal("expected auth validation error")
			}
		})
	}
}

func invalidBlobProbeCases() []invalidConfigCase {
	return []invalidConfigCase{
		{name: "blob policy", mutate: mutateHub(func(upstreamCfg *config.UpstreamConfig) {
			upstreamCfg.Blob.MirrorPolicy = "fastest"
		})},
		{name: "blob top n", mutate: mutateHub(func(upstreamCfg *config.UpstreamConfig) {
			upstreamCfg.Blob.TopN = -1
		})},
		{name: "blob max concurrency", mutate: mutateHub(func(upstreamCfg *config.UpstreamConfig) {
			upstreamCfg.Blob.MaxConcurrencyPerEndpoint = -1
		})},
		{name: "blob max concurrent attempts", mutate: mutateHub(func(upstreamCfg *config.UpstreamConfig) {
			upstreamCfg.Blob.MaxConcurrentAttempts = -1
		})},
		{name: "probe interval", mutate: mutateHub(func(upstreamCfg *config.UpstreamConfig) {
			upstreamCfg.Probe.Interval = -time.Second
		})},
		{name: "probe timeout", mutate: mutateHub(func(upstreamCfg *config.UpstreamConfig) {
			upstreamCfg.Probe.Timeout = -time.Second
		})},
		{name: "probe cooldown", mutate: mutateHub(func(upstreamCfg *config.UpstreamConfig) {
			upstreamCfg.Probe.Cooldown = -time.Second
		})},
		{name: "worker probe concurrency", mutate: func(cfg *config.Config) {
			cfg.Worker.ProbeConcurrency = -1
		}},
		{name: "worker prefetch concurrency", mutate: func(cfg *config.Config) {
			cfg.Worker.PrefetchConcurrency = -1
		}},
		{name: "cleanup max_scan", mutate: func(cfg *config.Config) {
			cfg.Scheduler.Cleanup.MaxScan = -1
		}},
		{name: "cleanup max bytes", mutate: func(cfg *config.Config) {
			cfg.Scheduler.Cleanup.MaxBytes = -1
		}},
		{name: "cleanup target above max", mutate: func(cfg *config.Config) {
			cfg.Scheduler.Cleanup.MaxBytes = 1024
			cfg.Scheduler.Cleanup.TargetBytes = 2048
		}},
	}
}

func mutateHub(mutator func(*config.UpstreamConfig)) func(*config.Config) {
	return func(cfg *config.Config) {
		upstreamCfg := cfg.Upstreams["hub"]
		mutator(&upstreamCfg)
		cfg.Upstreams["hub"] = upstreamCfg
	}
}

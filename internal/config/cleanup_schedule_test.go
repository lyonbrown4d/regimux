package config_test

import (
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func TestDefaultCleanupRunsAtThreeInShanghai(t *testing.T) {
	cleanup := config.DefaultConfig().Scheduler.Cleanup
	if cleanup.Interval != 0 || cleanup.Cron != "0 3 * * *" || cleanup.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected cleanup schedule: %+v", cleanup)
	}
	location, err := time.LoadLocation(cleanup.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	cron := gocron.NewDefaultCron(false)
	now := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	if err := cron.IsValid(cleanup.Cron, location, now); err != nil {
		t.Fatal(err)
	}
	next := cron.Next(now).In(location)
	if next.Hour() != 3 || next.Minute() != 0 || next.Day() != 16 {
		t.Fatalf("unexpected next cleanup: %s", next)
	}
}

func TestCleanupScheduleRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*config.SchedulerCleanupConfig)
	}{
		{"invalid cron", func(cfg *config.SchedulerCleanupConfig) { cfg.Cron = "invalid" }},
		{"invalid timezone", func(cfg *config.SchedulerCleanupConfig) { cfg.Timezone = "Invalid/Zone" }},
		{"missing schedule", func(cfg *config.SchedulerCleanupConfig) { cfg.Cron = "" }},
		{"invalid retention", func(cfg *config.SchedulerCleanupConfig) { cfg.UnusedFor = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			test.change(&cfg.Scheduler.Cleanup)
			if err := cfg.NormalizeAndValidate(); err == nil {
				t.Fatal("invalid cleanup configuration accepted")
			}
		})
	}
}

package config

import (
	"strings"
	"time"
	_ "time/tzdata" // Keep named cleanup timezones available in minimal containers.

	"github.com/go-co-op/gocron/v2"
	"github.com/go-playground/validator/v10"
)

func validateCleanupSchedule(sl validator.StructLevel, cleanup SchedulerCleanupConfig) {
	if cleanup.Enabled && cleanup.UnusedFor <= 0 {
		sl.ReportError(cleanup.UnusedFor, "unused_for", "UnusedFor", "gt", "0")
	}
	if cleanup.Interval > 0 {
		return
	}
	if strings.TrimSpace(cleanup.Cron) == "" {
		if cleanup.Enabled {
			sl.ReportError(cleanup.Cron, "cron", "Cron", "required_without", "interval")
		}
		return
	}
	zone := cleanup.Timezone
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		sl.ReportError(cleanup.Timezone, "timezone", "Timezone", "timezone", "")
		return
	}
	if err := gocron.NewDefaultCron(false).IsValid(cleanup.Cron, location, time.Now()); err != nil {
		sl.ReportError(cleanup.Cron, "cron", "Cron", "cron", "")
	}
}

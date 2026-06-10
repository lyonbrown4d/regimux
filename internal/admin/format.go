package admin

import (
	"time"

	"github.com/dustin/go-humanize"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/lo"
)

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func formatDuration(value time.Duration) string {
	if value <= 0 {
		return "-"
	}
	if value < time.Second {
		return value.Round(time.Millisecond).String()
	}
	return value.Round(time.Second).String()
}

func formatBytes(value int64) string {
	if value <= 0 {
		return "0 B"
	}
	return humanize.IBytes(uint64(value))
}

func formatLatency(snapshot ecosystem.EndpointHealthSnapshot) string {
	if !snapshot.HasLatency {
		return "-"
	}
	return formatDuration(snapshot.LatencyEWMA)
}

func formatCooldown(snapshot ecosystem.EndpointHealthSnapshot) string {
	if snapshot.CooldownUntil.IsZero() {
		return "-"
	}
	if snapshot.InCooldown {
		return "until " + formatTime(snapshot.CooldownUntil)
	}
	return "expired"
}

func formatDegraded(snapshot ecosystem.EndpointHealthSnapshot) string {
	if snapshot.DegradedUntil.IsZero() {
		return "-"
	}
	if snapshot.InDegraded {
		return "until " + formatTime(snapshot.DegradedUntil)
	}
	return "expired"
}

func formatSuccessRate(snapshot ecosystem.EndpointHealthSnapshot) string {
	if !snapshot.HasSuccessRate {
		return "-"
	}
	return humanize.FormatFloat("#,###.##", snapshot.SuccessRate*100) + "%"
}

func endpointStatus(snapshot ecosystem.EndpointHealthSnapshot) string {
	switch {
	case snapshot.InCooldown:
		return "cooldown"
	case snapshot.InDegraded:
		return "degraded"
	case snapshot.HasLatency:
		return "healthy"
	case !snapshot.LastFailureAt.IsZero():
		return "failing"
	default:
		return "unknown"
	}
}

func latestTime(values ...time.Time) time.Time {
	return lo.Latest(values...)
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

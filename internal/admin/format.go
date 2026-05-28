package admin

import (
	"fmt"
	"time"

	"github.com/lyonbrown4d/regimux/internal/upstream"
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
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	size := float64(value)
	index := 0
	for size >= 1024 && index < len(units)-1 {
		size /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d %s", value, units[index])
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
}

func formatLatency(snapshot upstream.EndpointHealthSnapshot) string {
	if !snapshot.HasLatency {
		return "-"
	}
	return formatDuration(snapshot.LatencyEWMA)
}

func formatCooldown(snapshot upstream.EndpointHealthSnapshot) string {
	if snapshot.CooldownUntil.IsZero() {
		return "-"
	}
	if snapshot.InCooldown {
		return "until " + formatTime(snapshot.CooldownUntil)
	}
	return "expired"
}

func endpointStatus(snapshot upstream.EndpointHealthSnapshot) string {
	switch {
	case snapshot.InCooldown:
		return "cooldown"
	case snapshot.HasLatency:
		return "healthy"
	case !snapshot.LastFailureAt.IsZero():
		return "failing"
	default:
		return "unknown"
	}
}

func latestTime(values ...time.Time) time.Time {
	var out time.Time
	for _, value := range values {
		if value.After(out) {
			out = value
		}
	}
	return out
}

func compareTimeDesc(left, right time.Time) int {
	switch {
	case left.Equal(right):
		return 0
	case left.After(right):
		return -1
	default:
		return 1
	}
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

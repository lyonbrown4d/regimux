package prefetch

import (
	"context"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/events"
)

func (s *Service) publishFillSkipped(ctx context.Context, alias, kind, reason string) {
	if s == nil || s.events == nil {
		return
	}
	if err := events.Publish(ctx, s.events, events.ContainerPullFill{
		Alias:  alias,
		Source: "prefetch",
		Kind:   kind,
		Status: "skipped",
		Reason: normalizePrefetchFillReason(reason),
	}); err != nil && s.logger != nil {
		s.logger.DebugContext(ctx, "publish container pull fill event failed", "error", err)
	}
}

func normalizePrefetchFillReason(reason string) string {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return "unknown"
	case strings.HasPrefix(reason, "failure backoff until "):
		return "failure_backoff"
	}
	reason = strings.ToLower(reason)
	replacer := strings.NewReplacer(
		" ", "_",
		"-", "_",
		".", "_",
		"/", "_",
		":", "_",
	)
	reason = replacer.Replace(reason)
	out := make([]rune, 0, len(reason))
	lastUnderscore := false
	for _, r := range reason {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			out = append(out, r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			out = append(out, '_')
			lastUnderscore = true
		}
	}
	normalized := strings.Trim(string(out), "_")
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

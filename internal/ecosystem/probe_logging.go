package ecosystem

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (p *EndpointProber) logEndpoint(ctx context.Context, target ProbeTarget, endpoint string, latency time.Duration, status int, err error) {
	if p == nil || p.logger == nil {
		return
	}
	args := []any{
		"ecosystem", target.Ecosystem,
		"alias", target.Alias,
		"registry", endpoint,
		"latency", latency,
	}
	if status > 0 {
		args = append(args, "status", status)
	}
	if err != nil {
		p.logger.DebugContext(ctx, "ecosystem endpoint probe failed", append(args, "error", err)...)
		return
	}
	p.logger.DebugContext(ctx, "ecosystem endpoint probe completed", args...)
}

func (p *EndpointProber) logSummary(ctx context.Context, target ProbeTarget, successes, failures int, err error) {
	if p == nil || p.logger == nil {
		return
	}
	args := []any{"ecosystem", target.Ecosystem, "alias", target.Alias, "successes", successes, "failures", failures}
	if err != nil {
		p.logger.WarnContext(ctx, "ecosystem probe failed for all endpoints", append(args, "error", err)...)
		return
	}
	p.logger.DebugContext(ctx, "ecosystem probe completed", args...)
}

func (p *EndpointProber) logPersistError(ctx context.Context, target ProbeTarget, endpoint string, err error) {
	if p == nil || p.logger == nil {
		return
	}
	p.logger.WarnContext(ctx, "persist ecosystem endpoint probe failed", "ecosystem", target.Ecosystem, "alias", target.Alias, "registry", endpoint, "error", err)
}

func (p *EndpointProber) logHealthSnapshot(
	ctx context.Context,
	target ProbeTarget,
	now time.Time,
	outcome string,
	record meta.EndpointHealthRecord,
	err error,
) {
	if p == nil || p.logger == nil || record.Registry == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	args := []any{
		"ecosystem", target.Ecosystem,
		"alias", target.Alias,
		"registry", record.Registry,
		"outcome", outcome,
		"latency_ewma", record.LatencyEWMA,
		"latency_samples", record.LatencySamples,
		"consecutive_failures", record.ConsecutiveFailures,
		"success_count", record.SuccessCount,
		"failure_count", record.FailureCount,
		"content_mismatch_count", record.ContentMismatchCount,
		"cooldown_until", record.CooldownUntil,
		"degraded_until", record.DegradedUntil,
		"in_cooldown", !record.CooldownUntil.IsZero() && now.Before(record.CooldownUntil),
		"in_degraded", !record.DegradedUntil.IsZero() && now.Before(record.DegradedUntil),
		"last_success_at", record.LastSuccessAt,
		"last_failure_at", record.LastFailureAt,
		"last_probe_at", record.LastProbeAt,
	}
	if err != nil {
		p.logger.WarnContext(ctx, "ecosystem endpoint health snapshot", append(args, "error", err)...)
		return
	}
	p.logger.DebugContext(ctx, "ecosystem endpoint health snapshot", args...)
}

func (p *EndpointProber) logProbeStarted(ctx context.Context, target ProbeTarget, endpointCount int, err error) {
	if p == nil || p.logger == nil {
		return
	}
	args := []any{
		"ecosystem", target.Ecosystem,
		"alias", target.Alias,
		"registry", target.Config.Registry,
		"endpoint_count", endpointCount,
		"probe_timeout", target.Config.Probe.Timeout,
	}
	if err != nil {
		p.logger.WarnContext(ctx, "ecosystem probe skipped", append(args, "error", err)...)
		return
	}
	p.logger.DebugContext(ctx, "ecosystem probe started", args...)
}

func nextLatencyEWMA(current time.Duration, samples int, latency time.Duration) time.Duration {
	if samples <= 0 || current <= 0 {
		return latency
	}
	return time.Duration(float64(current)*(1-endpointHealthAlpha) + float64(latency)*endpointHealthAlpha)
}

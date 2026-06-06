package scheduler

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) runProbe(ctx context.Context, prober ecosystem.Prober, target ecosystem.ProbeTarget) error {
	startedAt := time.Now()
	if r == nil {
		return nil
	}
	r.logProbeStarted(ctx, target)

	if prober == nil {
		return r.handleMissingProber(ctx, target, startedAt)
	}
	if err := r.waitProbeJitter(ctx, target); err != nil {
		return r.handleProbeJitterError(ctx, target, startedAt, err)
	}

	if err := prober.Probe(ctx, target); err != nil {
		return r.handleProbeRunError(ctx, target, startedAt, err)
	}

	r.logProbeCompleted(ctx, target, time.Since(startedAt).Milliseconds())
	r.observeEndpointHealth(ctx)
	r.observeJob(ctx, "probe", target.Alias, startedAt, nil)
	return nil
}

func (r *Runtime) handleProbeRunError(ctx context.Context, target ecosystem.ProbeTarget, startedAt time.Time, err error) error {
	err = oops.Wrapf(err, "run ecosystem probe job")
	return r.handleProbeFailure(ctx, target, startedAt, err)
}

func (r *Runtime) handleProbeFailure(ctx context.Context, target ecosystem.ProbeTarget, startedAt time.Time, err error) error {
	if r.logger != nil {
		r.logger.WarnContext(ctx, "ecosystem probe job failed", "ecosystem", target.Ecosystem, "alias", target.Alias, "registry", target.Config.Registry, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
	}
	r.observeEndpointHealth(ctx)
	r.observeJob(ctx, "probe", target.Alias, startedAt, err)
	return err
}

func (r *Runtime) logProbeStarted(ctx context.Context, target ecosystem.ProbeTarget) {
	if r.logger == nil {
		return
	}
	r.logger.DebugContext(
		ctx,
		"ecosystem probe job started",
		"ecosystem", target.Ecosystem,
		"alias", target.Alias,
		"registry", target.Config.Registry,
	)
}

func (r *Runtime) handleMissingProber(ctx context.Context, target ecosystem.ProbeTarget, startedAt time.Time) error {
	err := oops.In("scheduler").With("ecosystem", target.Ecosystem, "alias", target.Alias).Errorf("ecosystem prober is not configured")
	if r.logger != nil {
		r.logger.WarnContext(ctx, "ecosystem probe job skipped", "ecosystem", target.Ecosystem, "alias", target.Alias, "registry", target.Config.Registry, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
	}
	r.observeJob(ctx, "probe", target.Alias, startedAt, err)
	return err
}

func (r *Runtime) handleProbeJitterError(ctx context.Context, target ecosystem.ProbeTarget, startedAt time.Time, err error) error {
	if r.logger != nil {
		r.logger.WarnContext(ctx, "ecosystem probe job jitter failed", "ecosystem", target.Ecosystem, "alias", target.Alias, "duration_ms", time.Since(startedAt).Milliseconds(), "error", err)
	}
	r.observeJob(ctx, "probe", target.Alias, startedAt, err)
	return err
}

func (r *Runtime) logProbeCompleted(ctx context.Context, target ecosystem.ProbeTarget, durationMs int64) {
	if r.logger == nil {
		return
	}
	r.logger.InfoContext(ctx,
		"ecosystem probe job completed",
		"ecosystem", target.Ecosystem,
		"alias", target.Alias,
		"duration_ms", durationMs,
	)
}

func (r *Runtime) waitProbeJitter(ctx context.Context, target ecosystem.ProbeTarget) error {
	if r == nil {
		return nil
	}
	jitter := target.Config.Probe.Jitter
	if jitter <= 0 {
		return nil
	}
	delay := randomProbeJitterDelay(jitter)
	if delay <= 0 {
		return nil
	}
	if r.logger != nil {
		r.logger.DebugContext(ctx, "applying ecosystem probe jitter", "ecosystem", target.Ecosystem, "alias", target.Alias, "delay", delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "wait upstream probe jitter")
	}
}

func randomProbeJitterDelay(maxDelay time.Duration) time.Duration {
	if maxDelay <= 0 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(maxDelay)))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64())
}

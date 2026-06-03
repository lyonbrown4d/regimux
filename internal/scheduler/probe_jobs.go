package scheduler

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) registerProbe(ctx context.Context, scheduler gocron.Scheduler) error {
	var registerErr error
	r.probers().Range(func(_ int, prober ecosystem.Prober) bool {
		if err := r.registerProbeTargets(ctx, scheduler, prober); err != nil {
			registerErr = err
			return false
		}
		return true
	})
	return registerErr
}

func (r *Runtime) registerProbeTargets(ctx context.Context, scheduler gocron.Scheduler, prober ecosystem.Prober) error {
	if prober == nil {
		return nil
	}
	targets := prober.ProbeTargets()
	if targets == nil {
		return nil
	}
	var registerErr error
	targets.Range(func(_ int, target ecosystem.ProbeTarget) bool {
		if !probeTargetEnabled(target) {
			return true
		}
		registerErr = r.registerProbeTarget(ctx, scheduler, prober, target)
		return registerErr == nil
	})
	return registerErr
}

func probeTargetEnabled(target ecosystem.ProbeTarget) bool {
	probeCfg := target.Config.Probe
	return probeCfg.Enabled && probeCfg.Interval > 0
}

func (r *Runtime) registerProbeTarget(
	ctx context.Context,
	scheduler gocron.Scheduler,
	prober ecosystem.Prober,
	target ecosystem.ProbeTarget,
) error {
	probeCfg := target.Config.Probe
	jobProber := prober
	jobTarget := target
	options := []gocron.JobOption{
		gocron.WithName("regimux." + jobTarget.Ecosystem + ".probe." + jobTarget.Alias),
		gocron.WithTags("maintenance", "probe", jobTarget.Ecosystem, jobTarget.Alias),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithDisabledDistributedJobLocker(true),
		gocron.WithStartAt(gocron.WithStartImmediately()),
	}
	if _, err := scheduler.NewJob(
		gocron.DurationJob(probeCfg.Interval),
		gocron.NewTask(func(ctx context.Context) error {
			return r.runProbe(ctx, jobProber, jobTarget)
		}),
		options...,
	); err != nil {
		return oops.Wrapf(err, "register ecosystem probe job")
	}
	r.logger.InfoContext(ctx,
		"registered ecosystem probe job",
		"ecosystem", jobTarget.Ecosystem,
		"alias", jobTarget.Alias,
		"interval", probeCfg.Interval,
		"timeout", probeCfg.Timeout,
		"cooldown", probeCfg.Cooldown,
		"jitter", probeCfg.Jitter,
	)
	return nil
}

func (r *Runtime) runProbe(ctx context.Context, prober ecosystem.Prober, target ecosystem.ProbeTarget) error {
	startedAt := time.Now()
	if prober == nil {
		err := oops.In("scheduler").With("ecosystem", target.Ecosystem, "alias", target.Alias).Errorf("ecosystem prober is not configured")
		r.observeJob(ctx, "probe", target.Alias, startedAt, err)
		return err
	}
	if err := r.waitProbeJitter(ctx, target); err != nil {
		r.observeJob(ctx, "probe", target.Alias, startedAt, err)
		return err
	}
	if err := prober.Probe(ctx, target); err != nil {
		err = oops.Wrapf(err, "run ecosystem probe job")
		r.observeJob(ctx, "probe", target.Alias, startedAt, err)
		r.observeEndpointHealth(ctx)
		return err
	}
	r.logger.InfoContext(ctx,
		"ecosystem probe job completed",
		"ecosystem", target.Ecosystem,
		"alias", target.Alias,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	r.observeEndpointHealth(ctx)
	r.observeJob(ctx, "probe", target.Alias, startedAt, nil)
	return nil
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

package scheduler

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/samber/oops"
)

func (r *Runtime) registerEcosystemJobs(ctx context.Context, scheduler gocron.Scheduler) error {
	var registerErr error
	r.jobProviders().Range(func(_ int, provider ecosystem.JobProvider) bool {
		jobs := provider.Jobs()
		if jobs == nil {
			return true
		}
		jobs.Range(func(_ int, spec ecosystem.JobSpec) bool {
			registerErr = r.registerJobSpec(ctx, scheduler, spec)
			return registerErr == nil
		})
		return registerErr == nil
	})
	return registerErr
}

func (r *Runtime) registerJobSpec(ctx context.Context, scheduler gocron.Scheduler, spec ecosystem.JobSpec) error {
	if !spec.Enabled || spec.Interval <= 0 || spec.Run == nil {
		return nil
	}
	options := []gocron.JobOption{
		gocron.WithName(spec.Name),
		gocron.WithTags(jobSpecTags(spec).Values()...),
		gocron.WithContext(ctx),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	}
	if !spec.Distributed {
		options = append(options, gocron.WithDisabledDistributedJobLocker(true))
	}
	if spec.StartImmediately {
		options = append(options, gocron.WithStartAt(gocron.WithStartImmediately()))
	}
	if _, err := scheduler.NewJob(
		gocron.DurationJob(spec.Interval),
		gocron.NewTask(func(ctx context.Context) error {
			return r.runJobSpec(ctx, spec)
		}),
		options...,
	); err != nil {
		return oops.With("job", spec.Name).Wrapf(err, "register ecosystem job")
	}
	r.logger.InfoContext(ctx,
		"registered ecosystem job",
		"job", spec.Name,
		"kind", spec.Kind,
		"ecosystem", spec.Ecosystem,
		"alias", spec.Alias,
		"interval", spec.Interval,
		"distributed", spec.Distributed,
	)
	return nil
}

func (r *Runtime) runJobSpec(ctx context.Context, spec ecosystem.JobSpec) error {
	startedAt := time.Now()
	if err := r.waitJobSpecJitter(ctx, spec); err != nil {
		return r.handleJobSpecError(ctx, spec, startedAt, err)
	}
	result, err := spec.Run(ctx)
	if err != nil {
		return r.handleJobSpecError(ctx, spec, startedAt, err)
	}
	if result.PrefetchReport != nil {
		r.observePrefetchReport(ctx, result.PrefetchReport)
	}
	if result.CleanupReport != nil {
		r.observeCleanupReport(ctx, result.CleanupReport)
	}
	if spec.ObserveEndpointHealth {
		r.observeEndpointHealth(ctx)
	}
	if r.logger != nil {
		r.logger.InfoContext(ctx,
			"ecosystem job completed",
			"job", spec.Name,
			"kind", spec.Kind,
			"ecosystem", spec.Ecosystem,
			"alias", spec.Alias,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
	}
	r.observeJob(ctx, string(spec.Kind), spec.Alias, startedAt, nil)
	return nil
}

func (r *Runtime) handleJobSpecError(ctx context.Context, spec ecosystem.JobSpec, startedAt time.Time, err error) error {
	if r.logger != nil {
		r.logger.WarnContext(ctx,
			"ecosystem job failed",
			"job", spec.Name,
			"kind", spec.Kind,
			"ecosystem", spec.Ecosystem,
			"alias", spec.Alias,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"error", err,
		)
	}
	if spec.ObserveEndpointHealth {
		r.observeEndpointHealth(ctx)
	}
	r.observeJob(ctx, string(spec.Kind), spec.Alias, startedAt, err)
	return err
}

func (r *Runtime) waitJobSpecJitter(ctx context.Context, spec ecosystem.JobSpec) error {
	if spec.ProbeJitter <= 0 {
		return nil
	}
	delay := randomProbeJitterDelay(spec.ProbeJitter)
	if delay <= 0 {
		return nil
	}
	if r.logger != nil {
		r.logger.DebugContext(ctx, "applying ecosystem job jitter", "job", spec.Name, "delay", delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return oops.Wrapf(ctx.Err(), "wait ecosystem job jitter")
	}
}

func jobSpecTags(spec ecosystem.JobSpec) *collectionlist.List[string] {
	tags := collectionlist.NewList[string]()
	if spec.Tags != nil {
		tags.Add(spec.Tags.Values()...)
	}
	if tags.Len() == 0 {
		tags.Add("maintenance", string(spec.Kind))
	}
	return tags
}

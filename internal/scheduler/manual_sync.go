package scheduler

import (
	"context"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/samber/oops"
)

func (r *Runtime) SubmitSync(ctx context.Context, opts prefetch.SyncOptions) (prefetch.SyncJob, error) {
	if r == nil || r.prefetch == nil {
		return prefetch.SyncJob{}, oops.In("scheduler").Errorf("manual sync service is not configured")
	}
	job, err := r.prefetch.CreateSyncJob(ctx, opts)
	if err != nil {
		return prefetch.SyncJob{}, oops.Wrapf(err, "create manual sync job")
	}
	if r.scheduler == nil {
		err := oops.In("scheduler").Errorf("scheduler is not running")
		r.prefetch.MarkSyncJobFailed(job.ID, err)
		failed, ok := r.prefetch.SyncJob(job.ID)
		if ok {
			return failed, err
		}
		return job, err
	}

	if _, err := r.scheduler.NewJob(
		gocron.OneTimeJob(gocron.OneTimeJobStartImmediately()),
		gocron.NewTask(func(ctx context.Context) error {
			return r.runManualSync(ctx, job.ID)
		}),
		gocron.WithName("regimux.manual_sync."+job.ID),
		gocron.WithTags("manual-sync", opts.Alias),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithDisabledDistributedJobLocker(true),
	); err != nil {
		r.prefetch.MarkSyncJobFailed(job.ID, err)
		failed, ok := r.prefetch.SyncJob(job.ID)
		if ok {
			return failed, oops.Wrapf(err, "register manual sync job")
		}
		return job, oops.Wrapf(err, "register manual sync job")
	}
	r.logger.InfoContext(ctx,
		"manual sync job submitted",
		"job_id", job.ID,
		"alias", opts.Alias,
		"repository", opts.Repo,
		"reference", opts.Reference,
	)
	if current, ok := r.prefetch.SyncJob(job.ID); ok {
		return current, nil
	}
	return job, nil
}

func (r *Runtime) SyncJob(id string) (prefetch.SyncJob, bool) {
	if r == nil || r.prefetch == nil {
		return prefetch.SyncJob{}, false
	}
	return r.prefetch.SyncJob(id)
}

func (r *Runtime) runManualSync(ctx context.Context, id string) error {
	startedAt := time.Now()
	if r == nil || r.prefetch == nil {
		err := oops.In("scheduler").Errorf("manual sync service is not configured")
		r.observeJob(ctx, "manual_sync", "", startedAt, err)
		return err
	}
	job, ok := r.prefetch.SyncJob(id)
	if !ok {
		err := oops.In("scheduler").With("job_id", id).Errorf("manual sync job not found")
		r.observeJob(ctx, "manual_sync", "", startedAt, err)
		return err
	}
	err := r.prefetch.RunSyncJob(ctx, id)
	if err != nil {
		err = oops.Wrapf(err, "run manual sync job")
		r.logger.WarnContext(ctx,
			"manual sync job failed",
			"job_id", id,
			"alias", job.Options.Alias,
			"repository", job.Options.Repo,
			"reference", job.Options.Reference,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"error", err,
		)
		r.observeJob(ctx, "manual_sync", job.Options.Alias, startedAt, err)
		return err
	}
	r.logger.InfoContext(ctx,
		"manual sync job completed",
		"job_id", id,
		"alias", job.Options.Alias,
		"repository", job.Options.Repo,
		"reference", job.Options.Reference,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	r.observeJob(ctx, "manual_sync", job.Options.Alias, startedAt, nil)
	return nil
}

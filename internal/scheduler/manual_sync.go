package scheduler

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/samber/oops"
)

func (r *Runtime) SubmitSync(ctx context.Context, opts manualsync.SyncOptions) (manualsync.SyncJob, error) {
	if opts.Ecosystem == "" {
		return manualsync.SyncJob{}, oops.In("scheduler").Errorf("manual sync ecosystem is required")
	}
	syncer := r.manualSyncer(opts.Ecosystem)
	if syncer == nil {
		return manualsync.SyncJob{}, oops.In("scheduler").With("ecosystem", opts.Ecosystem).Errorf("manual sync service is not configured")
	}
	job, err := syncer.CreateSyncJob(ctx, opts)
	if err != nil {
		return manualsync.SyncJob{}, oops.Wrapf(err, "create manual sync job")
	}
	if r.scheduler == nil {
		err := oops.In("scheduler").Errorf("scheduler is not running")
		syncer.MarkSyncJobFailed(job.ID, err)
		failed, ok := syncer.SyncJob(job.ID)
		if ok {
			return failed, err
		}
		return job, err
	}

	localJob := false
	if _, err := registerImmediateJob(
		r.scheduler,
		func(ctx context.Context) error {
			return r.runManualSync(ctx, job.ID)
		},
		schedulerJobOptions{
			name:        "regimux.manual_sync." + job.ID,
			tags:        []string{"manual-sync", job.Options.Ecosystem, opts.Alias},
			distributed: &localJob,
		},
	); err != nil {
		syncer.MarkSyncJobFailed(job.ID, err)
		failed, ok := syncer.SyncJob(job.ID)
		if ok {
			return failed, oops.Wrapf(err, "register manual sync job")
		}
		return job, oops.Wrapf(err, "register manual sync job")
	}
	r.logger.InfoContext(ctx,
		"manual sync job submitted",
		"job_id", job.ID,
		"ecosystem", opts.Ecosystem,
		"alias", opts.Alias,
		"artifact", opts.Artifact,
		"reference", opts.Reference,
	)
	if current, ok := syncer.SyncJob(job.ID); ok {
		return current, nil
	}
	return job, nil
}

func (r *Runtime) SyncJob(id string) (manualsync.SyncJob, bool) {
	syncer, job, ok := r.manualSyncerByJob(id)
	if !ok || syncer == nil {
		return manualsync.SyncJob{}, false
	}
	return job, true
}

func (r *Runtime) runManualSync(ctx context.Context, id string) error {
	startedAt := time.Now()
	syncer, job, ok := r.manualSyncerByJob(id)
	if !ok || syncer == nil {
		err := oops.In("scheduler").Errorf("manual sync service is not configured")
		r.observeJob(ctx, "manual_sync", "", startedAt, err)
		return err
	}
	err := syncer.RunSyncJob(ctx, id)
	if err != nil {
		err = oops.Wrapf(err, "run manual sync job")
		r.logger.WarnContext(ctx,
			"manual sync job failed",
			"job_id", id,
			"ecosystem", job.Options.Ecosystem,
			"alias", job.Options.Alias,
			"artifact", job.Options.Artifact,
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
		"ecosystem", job.Options.Ecosystem,
		"alias", job.Options.Alias,
		"artifact", job.Options.Artifact,
		"reference", job.Options.Reference,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	r.observeJob(ctx, "manual_sync", job.Options.Alias, startedAt, nil)
	return nil
}

func (r *Runtime) manualSyncer(ecosystemName string) ecosystem.ManualSyncer {
	if r == nil || ecosystemName == "" {
		return nil
	}
	syncer, _ := namedRuntimeCapability[ecosystem.ManualSyncer](r, ecosystemName, matchRuntimeNameExact, continueAfterRuntimeMatch)
	return syncer
}

func (r *Runtime) manualSyncerByJob(id string) (ecosystem.ManualSyncer, manualsync.SyncJob, bool) {
	if r == nil || id == "" {
		return nil, manualsync.SyncJob{}, false
	}
	var job manualsync.SyncJob
	syncer, ok := runtimeCapabilities[ecosystem.ManualSyncer](r).FirstWhere(func(_ int, syncer ecosystem.ManualSyncer) bool {
		var found bool
		job, found = syncer.SyncJob(id)
		return found
	}).Get()
	if ok {
		return syncer, job, true
	}
	return nil, manualsync.SyncJob{}, false
}

package scheduler

import (
	"context"
	"strings"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/samber/oops"
)

type manualSyncer interface {
	CreateSyncJob(context.Context, prefetch.SyncOptions) (prefetch.SyncJob, error)
	RunSyncJob(context.Context, string) error
	MarkSyncJobFailed(string, error)
	SyncJob(string) (prefetch.SyncJob, bool)
}

func (r *Runtime) SubmitSync(ctx context.Context, opts prefetch.SyncOptions) (prefetch.SyncJob, error) {
	opts.Ecosystem = normalizeSyncEcosystem(opts.Ecosystem)
	syncer := r.manualSyncer(opts.Ecosystem)
	if syncer == nil {
		return prefetch.SyncJob{}, oops.In("scheduler").Errorf("manual sync service is not configured")
	}
	job, err := syncer.CreateSyncJob(ctx, opts)
	if err != nil {
		return prefetch.SyncJob{}, oops.Wrapf(err, "create manual sync job")
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

	if _, err := r.scheduler.NewJob(
		gocron.OneTimeJob(gocron.OneTimeJobStartImmediately()),
		gocron.NewTask(func(ctx context.Context) error {
			return r.runManualSync(ctx, job.ID)
		}),
		gocron.WithName("regimux.manual_sync."+job.ID),
		gocron.WithTags("manual-sync", job.Options.Ecosystem, opts.Alias),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithDisabledDistributedJobLocker(true),
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
		"repository", opts.Repo,
		"reference", opts.Reference,
	)
	if current, ok := syncer.SyncJob(job.ID); ok {
		return current, nil
	}
	return job, nil
}

func (r *Runtime) SyncJob(id string) (prefetch.SyncJob, bool) {
	syncer, job, ok := r.manualSyncerByJob(id)
	if !ok || syncer == nil {
		return prefetch.SyncJob{}, false
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
		"ecosystem", job.Options.Ecosystem,
		"alias", job.Options.Alias,
		"repository", job.Options.Repo,
		"reference", job.Options.Reference,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	r.observeJob(ctx, "manual_sync", job.Options.Alias, startedAt, nil)
	return nil
}

func (r *Runtime) manualSyncer(ecosystemName string) manualSyncer {
	if r == nil || ecosystemName == "" {
		return nil
	}
	var match manualSyncer
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		if runtime == nil || runtime.Name() != ecosystemName {
			return true
		}
		syncer, ok := runtime.(manualSyncer)
		if ok {
			match = syncer
			return false
		}
		return true
	})
	return match
}

func (r *Runtime) manualSyncerByJob(id string) (manualSyncer, prefetch.SyncJob, bool) {
	if r == nil || id == "" {
		return nil, prefetch.SyncJob{}, false
	}
	var matched manualSyncer
	var job prefetch.SyncJob
	var ok bool
	r.runtimes.Range(func(_ int, runtime ecosystem.Runtime) bool {
		if matched != nil {
			return false
		}
		syncer, isSyncer := runtime.(manualSyncer)
		if !isSyncer {
			return true
		}
		job, ok = syncer.SyncJob(id)
		if !ok {
			return true
		}
		matched = syncer
		return false
	})
	if matched != nil && ok {
		return matched, job, true
	}
	return nil, prefetch.SyncJob{}, false
}

func normalizeSyncEcosystem(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ecosystem.Container
	}
	return value
}

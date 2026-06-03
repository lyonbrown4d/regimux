package manualsync

import (
	"context"
	"log/slog"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/google/uuid"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/samber/oops"
)

const defaultSyncJobTimeout = 5 * time.Minute

type ExecuteFunc func(context.Context, prefetch.SyncOptions) (*prefetch.SyncReport, error)

type ServiceDependencies struct {
	Logger  *slog.Logger
	Timeout time.Duration
	Execute ExecuteFunc
}

type Service struct {
	execute  ExecuteFunc
	timeout  time.Duration
	logger   *slog.Logger
	syncJobs *collectionmapping.ConcurrentMap[string, prefetch.SyncJob]
}

func NewService(deps ServiceDependencies) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := deps.Timeout
	if timeout <= 0 {
		timeout = defaultSyncJobTimeout
	}
	return &Service{
		execute:  deps.Execute,
		timeout:  timeout,
		logger:   logger.With("component", "manual-sync"),
		syncJobs: collectionmapping.NewConcurrentMap[string, prefetch.SyncJob](),
	}
}

func (s *Service) CreateSyncJob(ctx context.Context, opts prefetch.SyncOptions) (prefetch.SyncJob, error) {
	if err := s.validate(ctx, opts); err != nil {
		return prefetch.SyncJob{}, err
	}
	job := prefetch.SyncJob{
		ID:        s.nextSyncJobID(),
		Status:    prefetch.SyncJobStatusQueued,
		Options:   opts,
		CreatedAt: time.Now().UTC(),
	}
	s.storeSyncJob(job)
	s.logger.InfoContext(ctx, "manual sync job queued",
		"job_id", job.ID,
		"ecosystem", opts.Ecosystem,
		"alias", opts.Alias,
		"repository", opts.Repo,
		"reference", opts.Reference,
	)
	return job, nil
}

func (s *Service) RunSyncJob(ctx context.Context, id string) error {
	if err := s.validateContext(ctx); err != nil {
		return err
	}
	job, ok := s.SyncJob(id)
	if !ok {
		return oops.In("manual-sync").With("job_id", id).Errorf("manual sync job not found")
	}
	startedAt := time.Now().UTC()
	s.updateSyncJob(id, func(job prefetch.SyncJob) prefetch.SyncJob {
		job.Status = prefetch.SyncJobStatusRunning
		job.StartedAt = startedAt
		return job
	})

	syncCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	report, err := s.execute(syncCtx, job.Options)
	finishedAt := time.Now().UTC()
	s.updateSyncJob(id, func(job prefetch.SyncJob) prefetch.SyncJob {
		job.FinishedAt = finishedAt
		if err != nil {
			job.Status = prefetch.SyncJobStatusFailed
			job.Error = err.Error()
			return job
		}
		job.Status = prefetch.SyncJobStatusSucceeded
		job.Result = report
		return job
	})
	return err
}

func (s *Service) MarkSyncJobFailed(id string, err error) {
	if err == nil {
		return
	}
	s.updateSyncJob(id, func(job prefetch.SyncJob) prefetch.SyncJob {
		job.Status = prefetch.SyncJobStatusFailed
		job.Error = err.Error()
		job.FinishedAt = time.Now().UTC()
		return job
	})
}

func (s *Service) SyncJob(id string) (prefetch.SyncJob, bool) {
	if s == nil || s.syncJobs == nil {
		return prefetch.SyncJob{}, false
	}
	return s.syncJobs.Get(id)
}

func (s *Service) nextSyncJobID() string {
	return "sync-" + uuid.NewString()
}

func (s *Service) storeSyncJob(job prefetch.SyncJob) {
	if s == nil {
		return
	}
	if s.syncJobs == nil {
		s.syncJobs = collectionmapping.NewConcurrentMap[string, prefetch.SyncJob]()
	}
	s.syncJobs.Set(job.ID, job)
}

func (s *Service) updateSyncJob(id string, update func(prefetch.SyncJob) prefetch.SyncJob) {
	if s == nil || update == nil {
		return
	}
	job, ok := s.SyncJob(id)
	if !ok {
		return
	}
	s.storeSyncJob(update(job))
}

func (s *Service) validate(ctx context.Context, opts prefetch.SyncOptions) error {
	if err := s.validateContext(ctx); err != nil {
		return err
	}
	if s == nil || s.execute == nil {
		return oops.In("manual-sync").Errorf("manual sync service is not configured")
	}
	if opts.Alias == "" {
		return oops.In("manual-sync").Errorf("manual sync upstream alias is required")
	}
	if opts.Repo == "" {
		return oops.In("manual-sync").Errorf("manual sync repository is required")
	}
	if opts.Reference == "" {
		return oops.In("manual-sync").Errorf("manual sync reference is required")
	}
	return nil
}

func (s *Service) validateContext(ctx context.Context) error {
	if ctx == nil {
		return oops.In("manual-sync").Errorf("manual sync context is required")
	}
	if err := ctx.Err(); err != nil {
		return oops.In("manual-sync").Wrapf(err, "manual sync context")
	}
	return nil
}

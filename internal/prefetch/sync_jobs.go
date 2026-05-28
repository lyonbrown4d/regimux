package prefetch

import (
	"context"
	"strconv"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

const (
	SyncJobStatusQueued    = "queued"
	SyncJobStatusRunning   = "running"
	SyncJobStatusSucceeded = "succeeded"
	SyncJobStatusFailed    = "failed"

	syncJobTimeout = 5 * time.Minute
)

type SyncJob struct {
	ID         string
	Status     string
	Options    SyncOptions
	Result     *SyncReport
	Error      string
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
}

func (s *Service) CreateSyncJob(ctx context.Context, opts SyncOptions) (SyncJob, error) {
	if err := s.validateSync(ctx, opts); err != nil {
		return SyncJob{}, err
	}
	if opts.Accept == "" {
		opts.Accept = distribution.DefaultManifestAccept
	}
	job := SyncJob{
		ID:        s.nextSyncJobID(),
		Status:    SyncJobStatusQueued,
		Options:   opts,
		CreatedAt: time.Now().UTC(),
	}
	s.storeSyncJob(job)
	s.logger.InfoContext(ctx, "manual sync job queued", "job_id", job.ID, "alias", opts.Alias, "repository", opts.Repo, "reference", opts.Reference)
	return job, nil
}

func (s *Service) RunSyncJob(ctx context.Context, id string) error {
	job, ok := s.SyncJob(id)
	if !ok {
		return oops.In("prefetch").With("job_id", id).Errorf("manual sync job not found")
	}
	startedAt := time.Now().UTC()
	s.updateSyncJob(id, func(job SyncJob) SyncJob {
		job.Status = SyncJobStatusRunning
		job.StartedAt = startedAt
		return job
	})

	syncCtx, cancel := context.WithTimeout(ctx, syncJobTimeout)
	defer cancel()
	report, err := s.Sync(syncCtx, job.Options)
	finishedAt := time.Now().UTC()
	s.updateSyncJob(id, func(job SyncJob) SyncJob {
		job.FinishedAt = finishedAt
		if err != nil {
			job.Status = SyncJobStatusFailed
			job.Error = err.Error()
			return job
		}
		job.Status = SyncJobStatusSucceeded
		job.Result = report
		return job
	})
	return err
}

func (s *Service) MarkSyncJobFailed(id string, err error) {
	if err == nil {
		return
	}
	s.updateSyncJob(id, func(job SyncJob) SyncJob {
		job.Status = SyncJobStatusFailed
		job.Error = err.Error()
		job.FinishedAt = time.Now().UTC()
		return job
	})
}

func (s *Service) SyncJob(id string) (SyncJob, bool) {
	if s == nil || s.syncJobs == nil {
		return SyncJob{}, false
	}
	return s.syncJobs.Get(id)
}

func (s *Service) nextSyncJobID() string {
	seq := s.syncJobSeq.Add(1)
	return "sync-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "-" + strconv.FormatInt(seq, 36)
}

func (s *Service) storeSyncJob(job SyncJob) {
	if s == nil {
		return
	}
	if s.syncJobs == nil {
		s.syncJobs = collectionmapping.NewConcurrentMap[string, SyncJob]()
	}
	s.syncJobs.Set(job.ID, job)
}

func (s *Service) updateSyncJob(id string, update func(SyncJob) SyncJob) {
	if s == nil || update == nil {
		return
	}
	job, ok := s.SyncJob(id)
	if !ok {
		return
	}
	s.storeSyncJob(update(job))
}

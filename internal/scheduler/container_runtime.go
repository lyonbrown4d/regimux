package scheduler

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/lyonbrown4d/regimux/internal/observability"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/upstream"
	"github.com/samber/oops"
)

type ContainerRuntime struct {
	cfg      config.Config
	upstream *upstream.Client
	prefetch *prefetch.Service
	manual   *manualsync.Service
}

func NewContainerRuntime(cfg config.Config, upstreamClient *upstream.Client, prefetchService *prefetch.Service) *ContainerRuntime {
	manual := manualsync.NewService(manualsync.ServiceDependencies{
		Execute: func(ctx context.Context, opts prefetch.SyncOptions) (*prefetch.SyncReport, error) {
			if prefetchService == nil {
				return nil, oops.In("scheduler").With("ecosystem", ecosystem.Container).Errorf("container manual sync service is not configured")
			}
			return prefetchService.Sync(ctx, opts)
		},
	})
	return &ContainerRuntime{
		cfg:      cfg,
		upstream: upstreamClient,
		prefetch: prefetchService,
		manual:   manual,
	}
}

func (r *ContainerRuntime) Name() string {
	return ecosystem.Container
}

func (r *ContainerRuntime) Upstreams() *collectionlist.List[ecosystem.Upstream] {
	if r == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	ordered := r.cfg.OrderedContainerUpstreams()
	out := collectionlist.NewList[ecosystem.Upstream]()
	ordered.Range(func(alias string, cfg config.UpstreamConfig) bool {
		out.Add(ecosystem.Upstream{
			Ecosystem: r.Name(),
			Alias:     alias,
			Config:    cfg,
		})
		return true
	})
	return out
}

func (r *ContainerRuntime) ProbeTargets() *collectionlist.List[ecosystem.ProbeTarget] {
	if r == nil {
		return collectionlist.NewList[ecosystem.ProbeTarget]()
	}
	upstreams := r.Upstreams()
	return collectionlist.FilterMapList(upstreams, func(_ int, upstream ecosystem.Upstream) (ecosystem.ProbeTarget, bool) {
		probeCfg := upstream.Config.Probe
		if !probeCfg.Enabled || probeCfg.Interval <= 0 {
			return ecosystem.ProbeTarget{}, false
		}
		return ecosystem.ProbeTarget(upstream), true
	})
}

func (r *ContainerRuntime) Probe(ctx context.Context, target ecosystem.ProbeTarget) error {
	if r == nil || r.upstream == nil {
		return oops.In("scheduler").With("ecosystem", ecosystem.Container).Errorf("container upstream probe client is not configured")
	}
	if err := r.upstream.ProbeAlias(ctx, target.Alias); err != nil {
		return oops.With("alias", target.Alias).Wrapf(err, "probe container upstream")
	}
	return nil
}

func (r *ContainerRuntime) FlushEndpointHealth(ctx context.Context) error {
	if r == nil || r.upstream == nil {
		return nil
	}
	if err := r.upstream.FlushEndpointHealth(ctx); err != nil {
		return oops.Wrapf(err, "flush container endpoint health")
	}
	return nil
}

func (r *ContainerRuntime) ObserveEndpointHealth(ctx context.Context, metrics *observability.Metrics) {
	if r == nil || r.upstream == nil || metrics == nil {
		return
	}
	metrics.ObserveUpstreamSnapshot(ctx, r.upstream.Snapshot(time.Now()))
}

func (r *ContainerRuntime) Prefetch(ctx context.Context, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if r == nil || r.prefetch == nil {
		return nil, oops.In("scheduler").With("ecosystem", ecosystem.Container).Errorf("container prefetch service is not configured")
	}
	report, err := r.prefetch.Run(ctx, prefetch.RunOptions{
		MaxRecords:           opts.MaxRecords,
		MinPullCount:         opts.MinPullCount,
		TagsPageSize:         opts.TagsPageSize,
		MaxCandidatesPerRepo: opts.MaxCandidatesPerRepo,
		MaxVersionDistance:   opts.MaxVersionDistance,
		Accept:               opts.Accept,
		MaxBytes:             opts.MaxBytes,
		MaxTasks:             opts.MaxTasks,
		MaxRepositories:      opts.MaxRepositories,
		FailureBackoff:       opts.FailureBackoff,
		RetryWindow:          opts.RetryWindow,
		Now:                  opts.Now,
		ManifestOnly:         opts.ManifestOnly,
	})
	if err != nil {
		return nil, oops.Wrapf(err, "run container prefetch")
	}
	return containerPrefetchReport(report), nil
}

func (r *ContainerRuntime) CreateSyncJob(ctx context.Context, opts prefetch.SyncOptions) (prefetch.SyncJob, error) {
	if r == nil || r.manual == nil {
		return prefetch.SyncJob{}, oops.In("scheduler").With("ecosystem", ecosystem.Container).Errorf("container manual sync service is not configured")
	}
	job, err := r.manual.CreateSyncJob(ctx, opts)
	if err != nil {
		return prefetch.SyncJob{}, oops.Wrapf(err, "create container manual sync job")
	}
	return job, nil
}

func (r *ContainerRuntime) RunSyncJob(ctx context.Context, id string) error {
	if r == nil || r.manual == nil {
		return oops.In("scheduler").With("ecosystem", ecosystem.Container).Errorf("container manual sync service is not configured")
	}
	if err := r.manual.RunSyncJob(ctx, id); err != nil {
		return oops.With("job_id", id).Wrapf(err, "run container manual sync job")
	}
	return nil
}

func (r *ContainerRuntime) MarkSyncJobFailed(id string, err error) {
	if r == nil || r.manual == nil {
		return
	}
	r.manual.MarkSyncJobFailed(id, err)
}

func (r *ContainerRuntime) SyncJob(id string) (prefetch.SyncJob, bool) {
	if r == nil || r.manual == nil {
		return prefetch.SyncJob{}, false
	}
	return r.manual.SyncJob(id)
}

func containerPrefetchReport(report *prefetch.RunReport) *ecosystem.PrefetchReport {
	if report == nil {
		return nil
	}
	return &ecosystem.PrefetchReport{
		Ecosystem:           ecosystem.Container,
		ScannedRecords:      report.ScannedRecords,
		SkippedRecords:      report.SkippedRecords,
		Repositories:        report.Repositories,
		SkippedRepositories: report.SkippedRepositories,
		Candidates:          report.Candidates,
		Prefetched:          report.Prefetched,
		Failed:              report.Failed,
		SkippedCandidates:   report.SkippedCandidates,
		BytesWarmed:         report.BytesWarmed,
		RetryRequested:      report.RetryRequested,
		Canceled:            report.Canceled,
	}
}

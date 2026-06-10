package container

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/prefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/samber/oops"
)

const endpointHealthFlushInterval = 2 * time.Second

func (r *Runtime) Jobs() *collectionlist.List[ecosystem.JobSpec] {
	jobs := collectionlist.NewList[ecosystem.JobSpec]()
	if r == nil {
		return jobs
	}
	r.addCleanupJob(jobs)
	r.addProbeJobs(jobs)
	r.addPrefetchJob(jobs)
	r.addManifestRefreshJob(jobs)
	r.addEndpointHealthFlushJob(jobs)
	return jobs
}

func (r *Runtime) addProbeJobs(jobs *collectionlist.List[ecosystem.JobSpec]) {
	r.ProbeTargets().Range(func(_ int, target ecosystem.ProbeTarget) bool {
		jobTarget := target
		jobs.Add(ecosystem.JobSpec{
			Name:                  "regimux." + jobTarget.Ecosystem + ".probe." + jobTarget.Alias,
			Kind:                  ecosystem.JobProbe,
			Ecosystem:             jobTarget.Ecosystem,
			Alias:                 jobTarget.Alias,
			Tags:                  collectionlist.NewList("maintenance", "probe", jobTarget.Ecosystem, jobTarget.Alias),
			Interval:              jobTarget.Config.Probe.Interval,
			Enabled:               true,
			Distributed:           false,
			StartImmediately:      true,
			ProbeJitter:           jobTarget.Config.Probe.Jitter,
			ObserveEndpointHealth: true,
			Run: func(ctx context.Context) (ecosystem.JobRunResult, error) {
				return ecosystem.JobRunResult{}, r.Probe(ctx, jobTarget)
			},
		})
		return true
	})
}

func (r *Runtime) addPrefetchJob(jobs *collectionlist.List[ecosystem.JobSpec]) {
	cfg := r.cfg.Scheduler.Prefetch
	jobs.Add(ecosystem.JobSpec{
		Name:        "regimux." + r.Name() + ".prefetch",
		Kind:        ecosystem.JobPrefetch,
		Ecosystem:   r.Name(),
		Tags:        collectionlist.NewList("maintenance", "prefetch", r.Name()),
		Interval:    cfg.Interval,
		Enabled:     cfg.Enabled && cfg.Interval > 0,
		Distributed: cfg.Distributed,
		Run: func(ctx context.Context) (ecosystem.JobRunResult, error) {
			report, err := r.Prefetch(ctx, ecosystem.PrefetchOptionsFromConfig(cfg, false))
			return ecosystem.JobRunResult{PrefetchReport: report}, err
		},
	})
}

func (r *Runtime) addManifestRefreshJob(jobs *collectionlist.List[ecosystem.JobSpec]) {
	cfg := r.cfg.Scheduler.ManifestRefresh.EffectiveFor(r.Name())
	jobs.Add(ecosystem.JobSpec{
		Name:        "regimux." + r.Name() + ".manifest_refresh",
		Kind:        ecosystem.JobManifestRefresh,
		Ecosystem:   r.Name(),
		Tags:        collectionlist.NewList("maintenance", "manifest-refresh", r.Name()),
		Interval:    cfg.Interval,
		Enabled:     cfg.Enabled && cfg.Interval > 0,
		Distributed: cfg.Distributed,
		Run: func(ctx context.Context) (ecosystem.JobRunResult, error) {
			report, err := r.Prefetch(ctx, ecosystem.PrefetchOptionsFromConfig(r.cfg.Scheduler.Prefetch, true))
			return ecosystem.JobRunResult{PrefetchReport: report}, err
		},
	})
}

func (r *Runtime) addEndpointHealthFlushJob(jobs *collectionlist.List[ecosystem.JobSpec]) {
	jobs.Add(ecosystem.JobSpec{
		Name:                  "regimux." + r.Name() + ".endpoint_health.flush",
		Kind:                  ecosystem.JobEndpointHealthFlush,
		Ecosystem:             r.Name(),
		Tags:                  collectionlist.NewList("maintenance", "endpoint-health", r.Name()),
		Interval:              endpointHealthFlushInterval,
		Enabled:               r.upstream != nil,
		Distributed:           false,
		ObserveEndpointHealth: true,
		Run: func(ctx context.Context) (ecosystem.JobRunResult, error) {
			return ecosystem.JobRunResult{}, r.FlushEndpointHealth(ctx)
		},
	})
}

func (r *Runtime) ProbeTargets() *collectionlist.List[ecosystem.ProbeTarget] {
	if r == nil {
		return collectionlist.NewList[ecosystem.ProbeTarget]()
	}
	return ecosystem.ProbeTargets(r.Upstreams())
}

func (r *Runtime) Probe(ctx context.Context, target ecosystem.ProbeTarget) error {
	if r == nil || r.upstream == nil {
		return oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container upstream probe client is not configured")
	}
	if err := r.upstream.ProbeAlias(ctx, target.Alias); err != nil {
		return oops.With("alias", target.Alias).Wrapf(err, "probe container upstream")
	}
	return nil
}

func (r *Runtime) FlushEndpointHealth(ctx context.Context) error {
	if r == nil || r.upstream == nil {
		return nil
	}
	if err := r.upstream.FlushEndpointHealth(ctx); err != nil {
		return oops.Wrapf(err, "flush container endpoint health")
	}
	return nil
}

func (r *Runtime) Prefetch(ctx context.Context, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if r == nil || r.prefetch == nil {
		return nil, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container prefetch service is not configured")
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

func (r *Runtime) Snapshot(now time.Time) ecosystem.ClientSnapshot {
	if r == nil || r.upstream == nil {
		return ecosystem.ClientSnapshot{
			Upstreams: collectionlist.NewList[ecosystem.UpstreamSnapshot](),
		}
	}

	raw := r.upstream.Snapshot(now)
	upstreams := collectionlist.MapList(raw.Upstreams, func(_ int, source upstream.UpstreamSnapshot) ecosystem.UpstreamSnapshot {
		return snapshotFromContainerSource(source)
	})
	return ecosystem.ClientSnapshot{Upstreams: upstreams}
}

func snapshotFromContainerSource(source upstream.UpstreamSnapshot) ecosystem.UpstreamSnapshot {
	out := ecosystem.UpstreamSnapshot{
		Ecosystem:  ecosystem.Container,
		Alias:      source.Alias,
		Policy:     source.Policy,
		BlobPolicy: source.BlobPolicy,
		Endpoints:  collectionlist.NewList[ecosystem.EndpointSnapshot](),
	}
	out.Endpoints = collectionlist.MapList(source.Endpoints, func(_ int, endpoint upstream.EndpointSnapshot) ecosystem.EndpointSnapshot {
		health := endpoint.Health
		return ecosystem.EndpointSnapshot{
			Registry: endpoint.Registry,
			Role:     endpoint.Role,
			Health: ecosystem.EndpointHealthSnapshot{
				Registry:             health.Registry,
				LatencyEWMA:          health.LatencyEWMA,
				LatencySamples:       health.LatencySamples,
				HasLatency:           health.HasLatency,
				ConsecutiveFailures:  health.ConsecutiveFailures,
				CooldownUntil:        health.CooldownUntil,
				DegradedUntil:        health.DegradedUntil,
				Inflight:             health.Inflight,
				LastSuccessAt:        health.LastSuccessAt,
				LastFailureAt:        health.LastFailureAt,
				LastProbeAt:          health.LastProbeAt,
				SuccessCount:         health.SuccessCount,
				FailureCount:         health.FailureCount,
				ContentMismatchCount: health.ContentMismatchCount,
				HasSuccessRate:       health.HasSuccessRate,
				SuccessRate:          health.SuccessRate,
				Score:                health.Score,
				InCooldown:           health.InCooldown,
				InDegraded:           health.InDegraded,
			},
		}
	})
	return out
}

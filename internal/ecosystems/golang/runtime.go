package golang

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/depprefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/samber/oops"
)

type runtimeAdapter struct {
	service    *Service
	prober     *ecosystem.EndpointProber
	prefetcher *depprefetch.Service
	manualSync *manualsync.Service
}

func newRuntimeAdapter(service *Service, prober *ecosystem.EndpointProber, metadata meta.Store, pools *worker.Pools, logger *slog.Logger) *runtimeAdapter {
	adapter := &runtimeAdapter{
		service: service,
		prober:  prober,
	}
	adapter.manualSync = manualsync.NewService(manualsync.ServiceDependencies{
		Execute: func(ctx context.Context, opts prefetch.SyncOptions) (*prefetch.SyncReport, error) {
			return adapter.syncDependency(ctx, opts)
		},
	})
	adapter.prefetcher = depprefetch.New(depprefetch.Dependencies{
		Ecosystem: ecosystem.Go,
		Metadata:  metadata,
		Workers:   pools,
		Logger:    logger,
		Fetch:     adapter.prefetch,
	})
	return adapter
}

func (r *runtimeAdapter) Name() string {
	return ecosystem.Go
}

func (r *runtimeAdapter) Upstreams() *collectionlist.List[ecosystem.Upstream] {
	if r == nil || r.service == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	upstreams := r.service.Upstreams()
	return collectionlist.MapList(upstreams, func(_ int, upstream Upstream) ecosystem.Upstream {
		return ecosystem.Upstream{
			Ecosystem: r.Name(),
			Alias:     upstream.Alias,
			Config:    upstream.Config,
		}
	})
}

func (r *runtimeAdapter) UpstreamAliases() *collectionlist.List[string] {
	return ecosystem.UpstreamAliases(r.Upstreams())
}

func (r *runtimeAdapter) Jobs() *collectionlist.List[ecosystem.JobSpec] {
	jobs := ecosystem.ProbeJobSpecs(r)
	if r == nil || r.service == nil {
		return jobs
	}
	jobs.Add(ecosystem.PrefetchJobSpec(r, r.service.cfg.Scheduler.Prefetch))
	jobs.Add(ecosystem.ManifestRefreshJobSpec(r, r.service.cfg.Scheduler.ManifestRefresh, r.service.cfg.Scheduler.Prefetch))
	return jobs
}

func (r *runtimeAdapter) ProbeCapability() ecosystem.Capability {
	return ecosystem.ProbeCapability(r.Upstreams())
}

func (r *runtimeAdapter) PrefetchCapability() ecosystem.Capability {
	return depprefetch.Capability(r.Name(), r.Upstreams())
}

func (r *runtimeAdapter) ProbeTargets() *collectionlist.List[ecosystem.ProbeTarget] {
	return ecosystem.ProbeTargets(r.Upstreams())
}

func (r *runtimeAdapter) Prefetch(ctx context.Context, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if r == nil || r.prefetcher == nil {
		return nil, oops.In("go-proxy").Errorf("go proxy prefetcher is not configured")
	}
	report, err := r.prefetcher.Prefetch(ctx, opts)
	if err != nil {
		return report, oops.Wrapf(err, "prefetch go proxy artifacts")
	}
	return report, nil
}

func (r *runtimeAdapter) prefetch(ctx context.Context, candidate depprefetch.Candidate) (depprefetch.FetchResult, error) {
	tail := candidate.Repository + "/" + candidate.Reference
	resp, err := r.service.Get(ctx, Request{
		Alias:          candidate.Alias,
		Tail:           tail,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return depprefetch.FetchResult{}, err
	}
	defer closeResponseBody(resp)
	if resp.Cache != cacheMiss {
		return depprefetch.FetchResult{}, nil
	}
	if resp.Body != nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return depprefetch.FetchResult{}, oops.Wrapf(err, "drain go proxy prefetch response")
		}
	}
	return depprefetch.FetchResult{BytesWarmed: resp.Size}, nil
}

func (r *runtimeAdapter) Probe(ctx context.Context, target ecosystem.ProbeTarget) error {
	if r == nil || r.prober == nil {
		return oops.In("go-proxy").Errorf("go proxy endpoint prober is not configured")
	}
	if err := r.prober.Probe(ctx, target); err != nil {
		return oops.Wrapf(err, "probe go proxy upstream")
	}
	return nil
}

func (r *runtimeAdapter) CreateSyncJob(ctx context.Context, opts prefetch.SyncOptions) (prefetch.SyncJob, error) {
	if r == nil || r.manualSync == nil {
		return prefetch.SyncJob{}, oops.In("go-proxy").Errorf("go proxy manual sync service is not configured")
	}
	job, err := r.manualSync.CreateSyncJob(ctx, opts)
	if err != nil {
		return prefetch.SyncJob{}, oops.Wrapf(err, "create go proxy manual sync job")
	}
	return job, nil
}

func (r *runtimeAdapter) RunSyncJob(ctx context.Context, id string) error {
	if r == nil || r.manualSync == nil {
		return oops.In("go-proxy").Errorf("go proxy manual sync service is not configured")
	}
	if err := r.manualSync.RunSyncJob(ctx, id); err != nil {
		return oops.With("job_id", id).Wrapf(err, "run go proxy manual sync job")
	}
	return nil
}

func (r *runtimeAdapter) MarkSyncJobFailed(id string, err error) {
	if r == nil || r.manualSync == nil {
		return
	}
	r.manualSync.MarkSyncJobFailed(id, err)
}

func (r *runtimeAdapter) SyncJob(id string) (prefetch.SyncJob, bool) {
	if r == nil || r.manualSync == nil {
		return prefetch.SyncJob{}, false
	}
	return r.manualSync.SyncJob(id)
}

func (r *runtimeAdapter) syncDependency(ctx context.Context, opts prefetch.SyncOptions) (*prefetch.SyncReport, error) {
	if r == nil || r.service == nil {
		return nil, oops.In("go-proxy").Errorf("go proxy manual sync service is not configured")
	}
	resp, err := r.service.Get(ctx, Request{
		Alias:          opts.Alias,
		Tail:           opts.Repo + "/" + opts.Reference,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return nil, err
	}
	defer closeResponseBody(resp)
	if resp == nil {
		return nil, oops.In("go-proxy").Errorf("go proxy manual sync response is empty")
	}
	if resp.Status < http.StatusOK || resp.Status >= http.StatusMultipleChoices {
		return nil, oops.In("go-proxy").With("status", resp.Status).Errorf("manual sync request failed")
	}
	bytesWarmed, copyErr := io.Copy(io.Discard, resp.Body)
	if copyErr != nil {
		return nil, oops.With("status", resp.Status).Wrapf(copyErr, "drain go proxy manual sync response")
	}
	// go proxy has a dynamic artifact surface; report only common fields.
	return &prefetch.SyncReport{
		Alias:     opts.Alias,
		Repo:      opts.Repo,
		Reference: opts.Reference,
		BlobCount: int(bytesWarmed),
	}, nil
}

var _ ecosystem.Runtime = (*runtimeAdapter)(nil)
var _ ecosystem.UpstreamProvider = (*runtimeAdapter)(nil)
var _ ecosystem.UpstreamAliasProvider = (*runtimeAdapter)(nil)
var _ ecosystem.CapabilityProvider = (*runtimeAdapter)(nil)
var _ ecosystem.Prober = (*runtimeAdapter)(nil)
var _ ecosystem.Prefetcher = (*runtimeAdapter)(nil)
var _ ecosystem.JobProvider = (*runtimeAdapter)(nil)

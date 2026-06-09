package golang

import (
	"context"
	"log/slog"
	"net/http"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/depprefetch"
	"github.com/lyonbrown4d/regimux/internal/depruntime"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
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
		Execute: func(ctx context.Context, opts manualsync.SyncOptions) (*manualsync.SyncReport, error) {
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
	return depruntime.Upstreams(r.Name(), r.service.Upstreams(), func(upstream Upstream) string {
		return upstream.Alias
	}, func(upstream Upstream) config.UpstreamConfig {
		return upstream.Config
	})
}

func (r *runtimeAdapter) UpstreamAliases() *collectionlist.List[string] {
	return ecosystem.UpstreamAliases(r.Upstreams())
}

func (r *runtimeAdapter) Jobs() *collectionlist.List[ecosystem.JobSpec] {
	if r == nil || r.service == nil {
		return ecosystem.ProbeJobSpecs(r)
	}
	return depruntime.Jobs(r, true, r.service.cfg.Scheduler.Prefetch, r.service.cfg.Scheduler.ManifestRefresh)
}

func (r *runtimeAdapter) ProbeCapability() ecosystem.Capability {
	return ecosystem.ProbeCapability(r.Upstreams())
}

func (r *runtimeAdapter) PrefetchCapability() ecosystem.Capability {
	return depprefetch.Capability(r.Name(), r.Upstreams())
}

func (r *runtimeAdapter) ManualSyncCapability() ecosystem.Capability {
	if r == nil {
		return ecosystem.DisabledCapability("go proxy manual sync service is not configured", r.Upstreams())
	}
	return depruntime.ManualSyncCapability(r.Name(), "go", r.manualSync != nil, r.Upstreams())
}

func (r *runtimeAdapter) ProbeTargets() *collectionlist.List[ecosystem.ProbeTarget] {
	return ecosystem.ProbeTargets(r.Upstreams())
}

func (r *runtimeAdapter) Prefetch(ctx context.Context, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if r == nil {
		return nil, oops.In(ecosystem.Go).Errorf("go proxy prefetcher is not configured")
	}
	report, err := depruntime.RunPrefetch(ctx, r.Name(), "go", r.prefetcher, opts)
	if err != nil {
		return report, oops.Wrapf(err, "prefetch go proxy artifacts")
	}
	return report, nil
}

func (r *runtimeAdapter) prefetch(ctx context.Context, candidate depprefetch.Candidate) (depprefetch.FetchResult, error) {
	tail := candidate.Repository + "/" + candidate.Reference
	resp, err := r.service.refresh(ctx, Request{
		Alias:          candidate.Alias,
		Tail:           tail,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return depprefetch.FetchResult{}, err
	}
	defer closeResponseBody(resp)
	if resp == nil {
		return depprefetch.FetchResult{}, oops.In("go").Errorf("go proxy prefetch response is empty")
	}
	result, err := depruntime.PrefetchResult("go proxy", resp.Cache, resp.Size, resp.Body)
	if err != nil {
		return result, oops.Wrapf(err, "drain go proxy prefetch response")
	}
	return result, nil
}

func (r *runtimeAdapter) Probe(ctx context.Context, target ecosystem.ProbeTarget) error {
	if r == nil || r.prober == nil {
		return oops.In("go").Errorf("go proxy endpoint prober is not configured")
	}
	if err := r.prober.Probe(ctx, target); err != nil {
		return oops.Wrapf(err, "probe go proxy upstream")
	}
	return nil
}

func (r *runtimeAdapter) CreateSyncJob(ctx context.Context, opts manualsync.SyncOptions) (manualsync.SyncJob, error) {
	if r == nil {
		return manualsync.SyncJob{}, oops.In("go").Errorf("go proxy manual sync service is not configured")
	}
	job, err := depruntime.CreateSyncJob(ctx, r.Name(), "go", r.manualSync, opts)
	if err != nil {
		return manualsync.SyncJob{}, oops.Wrapf(err, "create go proxy manual sync job")
	}
	return job, nil
}

func (r *runtimeAdapter) RunSyncJob(ctx context.Context, id string) error {
	if r == nil {
		return oops.In("go").Errorf("go proxy manual sync service is not configured")
	}
	if err := depruntime.RunSyncJob(ctx, r.Name(), "go", r.manualSync, id); err != nil {
		return oops.Wrapf(err, "run go proxy manual sync job")
	}
	return nil
}

func (r *runtimeAdapter) MarkSyncJobFailed(id string, err error) {
	if r == nil {
		return
	}
	depruntime.MarkSyncJobFailed(r.manualSync, id, err)
}

func (r *runtimeAdapter) SyncJob(id string) (manualsync.SyncJob, bool) {
	if r == nil {
		return manualsync.SyncJob{}, false
	}
	return depruntime.SyncJob(r.manualSync, id)
}

func (r *runtimeAdapter) syncDependency(ctx context.Context, opts manualsync.SyncOptions) (*manualsync.SyncReport, error) {
	if r == nil || r.service == nil {
		return nil, oops.In("go").Errorf("go proxy manual sync service is not configured")
	}
	resp, err := r.service.refresh(ctx, Request{
		Alias:          opts.Alias,
		Tail:           opts.Artifact + "/" + opts.Reference,
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return nil, err
	}
	defer closeResponseBody(resp)
	if resp == nil {
		return nil, oops.In("go").Errorf("go proxy manual sync response is empty")
	}
	bytesWarmed, err := depruntime.DrainSuccessful(r.Name(), "manual sync", "go proxy manual sync", resp.Status, resp.Body)
	if err != nil {
		return nil, oops.Wrapf(err, "drain go proxy manual sync response")
	}
	// go proxy has a dynamic artifact surface; report only common fields.
	return &manualsync.SyncReport{
		Alias:       opts.Alias,
		Artifact:    opts.Artifact,
		Reference:   opts.Reference,
		BytesWarmed: bytesWarmed,
	}, nil
}

func (r *runtimeAdapter) Refresh(ctx context.Context, req ecosystem.RefreshRequest) error {
	if r == nil || r.service == nil {
		return oops.In("go").Errorf("go proxy refresh service is not configured")
	}
	resp, err := r.service.refresh(ctx, Request{
		Alias:  req.Alias,
		Tail:   req.Repository + "/" + req.Reference,
		Method: http.MethodGet,
	})
	if err != nil {
		return err
	}
	defer closeResponseBody(resp)
	if resp == nil {
		return oops.In("go").Errorf("go proxy refresh response is empty")
	}
	_, err = depruntime.DrainSuccessful(r.Name(), "go proxy refresh", "go proxy refresh", resp.Status, resp.Body)
	if err != nil {
		return oops.Wrapf(err, "drain go proxy refresh response")
	}
	return nil
}

var _ ecosystem.Runtime = (*runtimeAdapter)(nil)
var _ ecosystem.UpstreamProvider = (*runtimeAdapter)(nil)
var _ ecosystem.UpstreamAliasProvider = (*runtimeAdapter)(nil)
var _ ecosystem.CapabilityProvider = (*runtimeAdapter)(nil)
var _ ecosystem.Prober = (*runtimeAdapter)(nil)
var _ ecosystem.Prefetcher = (*runtimeAdapter)(nil)
var _ ecosystem.JobProvider = (*runtimeAdapter)(nil)
var _ ecosystem.ManualSyncer = (*runtimeAdapter)(nil)
var _ ecosystem.Refresher = (*runtimeAdapter)(nil)

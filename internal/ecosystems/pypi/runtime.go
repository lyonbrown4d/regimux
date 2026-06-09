package pypi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

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
	adapter := &runtimeAdapter{service: service, prober: prober}
	adapter.manualSync = manualsync.NewService(manualsync.ServiceDependencies{
		Execute: func(ctx context.Context, opts manualsync.SyncOptions) (*manualsync.SyncReport, error) {
			return adapter.syncDependency(ctx, opts)
		},
	})
	adapter.prefetcher = depprefetch.New(depprefetch.Dependencies{
		Ecosystem: ecosystem.PyPI,
		Metadata:  metadata,
		Workers:   pools,
		Logger:    logger,
		Fetch:     adapter.prefetch,
	})
	return adapter
}

func (r *runtimeAdapter) Name() string {
	return ecosystemPyPI
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
		return ecosystem.DisabledCapability("pypi proxy manual sync service is not configured", r.Upstreams())
	}
	return depruntime.ManualSyncCapability(r.Name(), "pypi", r.manualSync != nil, r.Upstreams())
}

func (r *runtimeAdapter) ProbeTargets() *collectionlist.List[ecosystem.ProbeTarget] {
	return ecosystem.ProbeTargets(r.Upstreams())
}

func (r *runtimeAdapter) Prefetch(ctx context.Context, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if r == nil {
		return nil, oops.In("pypi").Errorf("pypi proxy prefetcher is not configured")
	}
	report, err := depruntime.RunPrefetch(ctx, r.Name(), "pypi", r.prefetcher, opts)
	if err != nil {
		return report, oops.Wrapf(err, "prefetch pypi proxy artifacts")
	}
	return report, nil
}

func (r *runtimeAdapter) prefetch(ctx context.Context, candidate depprefetch.Candidate) (depprefetch.FetchResult, error) {
	resp, err := r.service.refresh(ctx, Request{
		Alias:          candidate.Alias,
		Tail:           pypiTail(candidate),
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return depprefetch.FetchResult{}, err
	}
	if resp == nil {
		return depprefetch.FetchResult{}, oops.In("pypi").Errorf("pypi proxy prefetch response is empty")
	}
	defer closeReadCloser(resp.Body, nil, "close pypi prefetch response body")
	result, err := depruntime.PrefetchResult("pypi proxy", resp.Cache, resp.Size, resp.Body)
	if err != nil {
		return result, oops.Wrapf(err, "drain pypi prefetch response")
	}
	return result, nil
}

func pypiTail(candidate depprefetch.Candidate) string {
	if normalized, ok := strings.CutPrefix(candidate.Repository, "pypi/simple/"); ok {
		return "simple/" + normalized + "/"
	}
	return "packages/" + candidate.Reference
}

func (r *runtimeAdapter) Probe(ctx context.Context, target ecosystem.ProbeTarget) error {
	if r == nil || r.prober == nil {
		return oops.In("pypi").Errorf("pypi proxy endpoint prober is not configured")
	}
	if err := r.prober.Probe(ctx, target); err != nil {
		return oops.Wrapf(err, "probe pypi proxy upstream")
	}
	return nil
}

func (r *runtimeAdapter) CreateSyncJob(ctx context.Context, opts manualsync.SyncOptions) (manualsync.SyncJob, error) {
	if r == nil {
		return manualsync.SyncJob{}, oops.In("pypi").Errorf("pypi proxy manual sync service is not configured")
	}
	job, err := depruntime.CreateSyncJob(ctx, r.Name(), "pypi", r.manualSync, opts)
	if err != nil {
		return manualsync.SyncJob{}, oops.Wrapf(err, "create pypi proxy manual sync job")
	}
	return job, nil
}

func (r *runtimeAdapter) RunSyncJob(ctx context.Context, id string) error {
	if r == nil {
		return oops.In("pypi").Errorf("pypi proxy manual sync service is not configured")
	}
	if err := depruntime.RunSyncJob(ctx, r.Name(), "pypi", r.manualSync, id); err != nil {
		return oops.Wrapf(err, "run pypi proxy manual sync job")
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
		return nil, oops.In("pypi").Errorf("pypi proxy manual sync service is not configured")
	}
	resp, err := r.service.refresh(ctx, Request{
		Alias:          opts.Alias,
		Tail:           pypiTail(depprefetch.Candidate{Alias: opts.Alias, Repository: opts.Artifact, Reference: opts.Reference}),
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, oops.In("pypi").Errorf("pypi proxy manual sync response is empty")
	}
	defer closeReadCloser(resp.Body, nil, "close pypi manual sync response body")
	bytesWarmed, err := depruntime.DrainSuccessful(r.Name(), "manual sync", "pypi manual sync", resp.Status, resp.Body)
	if err != nil {
		return nil, oops.Wrapf(err, "drain pypi manual sync response")
	}
	return &manualsync.SyncReport{
		Alias:       opts.Alias,
		Artifact:    opts.Artifact,
		Reference:   opts.Reference,
		BytesWarmed: bytesWarmed,
	}, nil
}

func (r *runtimeAdapter) Refresh(ctx context.Context, req ecosystem.RefreshRequest) error {
	if r == nil || r.service == nil {
		return oops.In("pypi").Errorf("pypi proxy refresh service is not configured")
	}
	resp, err := r.service.refresh(ctx, Request{
		Alias:  req.Alias,
		Tail:   "simple/" + strings.TrimPrefix(req.Repository, "pypi/simple/") + "/",
		Method: http.MethodGet,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return oops.In("pypi").Errorf("pypi proxy refresh response is empty")
	}
	defer closeReadCloser(resp.Body, nil, "close pypi refresh response body")
	_, err = depruntime.DrainSuccessful(r.Name(), "pypi proxy refresh", "pypi refresh", resp.Status, resp.Body)
	if err != nil {
		return oops.Wrapf(err, "drain pypi refresh response")
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

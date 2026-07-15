package dist

import (
	"context"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/depprefetch"
	"github.com/lyonbrown4d/regimux/internal/depruntime"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/samber/oops"
	"log/slog"
	"net/http"
)

type runtimeAdapter struct {
	*depruntime.Adapter
	service *Service
}

func newRuntimeAdapter(service *Service, prober *ecosystem.EndpointProber, metadata meta.Store, pools *worker.Pools, logger *slog.Logger) *runtimeAdapter {
	adapter := &runtimeAdapter{service: service}
	options := depruntime.AdapterOptions[Upstream]{
		Name:              ecosystem.Dist,
		Label:             "dist",
		UpstreamAlias:     func(upstream Upstream) string { return upstream.Alias },
		UpstreamConfig:    func(upstream Upstream) config.UpstreamConfig { return upstream.Config },
		ServiceConfigured: service != nil,
		Prober:            prober,
		Metadata:          metadata,
		Workers:           pools,
		Logger:            logger,
		Fetch:             adapter.prefetch,
		Sync:              adapter.syncDist,
	}
	if service != nil {
		options.Upstreams = service.Upstreams
		options.PrefetchSchedule = service.cfg.Scheduler.Prefetch
		options.RefreshSchedule = service.cfg.Scheduler.ManifestRefresh

	}
	adapter.Adapter = depruntime.NewAdapter(options)
	return adapter
}
func (r *runtimeAdapter) prefetch(ctx context.Context, candidate depprefetch.Candidate) (depprefetch.FetchResult, error) {
	resp, err := r.service.refresh(ctx, Request{
		Alias:          candidate.Alias,
		Tail:           distTail(candidate.Repository, candidate.Reference),
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return depprefetch.FetchResult{}, err
	}
	if resp == nil {
		return depprefetch.FetchResult{}, oops.In("dist").Errorf("dist prefetch response is empty")
	}
	defer closeReadCloser(resp.Body, nil, "close dist prefetch response body")
	result, err := depruntime.PrefetchResult("dist mirror", resp.Cache, resp.Size, resp.Body)
	if err != nil {
		return result, oops.Wrapf(err, "drain dist prefetch response")
	}
	return result, nil
}

func (r *runtimeAdapter) syncDist(ctx context.Context, opts manualsync.SyncOptions) (*manualsync.SyncReport, error) {
	if r == nil || r.service == nil {
		return nil, oops.In("dist").Errorf("dist manual sync service is not configured")
	}
	resp, err := r.service.refresh(ctx, Request{
		Alias:          opts.Alias,
		Tail:           distTail(opts.Artifact, opts.Reference),
		Method:         http.MethodGet,
		SkipPullRecord: true,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, oops.In("dist").Errorf("dist manual sync response is empty")
	}
	defer closeReadCloser(resp.Body, nil, "close dist manual sync response body")
	bytesWarmed, err := depruntime.DrainSuccessful(r.Name(), "manual sync", "dist manual sync", resp.Status, resp.Body)
	if err != nil {
		return nil, oops.Wrapf(err, "drain dist manual sync response")
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
		return oops.In("dist").Errorf("dist refresh service is not configured")
	}
	resp, err := r.service.refresh(ctx, Request{
		Alias:  req.Alias,
		Tail:   distTail(req.Repository, req.Reference),
		Method: http.MethodGet,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return oops.In("dist").Errorf("dist refresh response is empty")
	}
	defer closeReadCloser(resp.Body, nil, "close dist refresh response body")
	_, err = depruntime.DrainSuccessful(r.Name(), "dist refresh", "dist refresh", resp.Status, resp.Body)
	if err != nil {
		return oops.Wrapf(err, "drain dist refresh response")
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

func distTail(repository, reference string) string {
	if repository == "" || repository == "dist" {
		return reference
	}
	return repository + "/" + reference
}

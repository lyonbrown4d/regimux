package golang

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
		Name:              ecosystem.Go,
		Label:             "go",
		UpstreamAlias:     func(upstream Upstream) string { return upstream.Alias },
		UpstreamConfig:    func(upstream Upstream) config.UpstreamConfig { return upstream.Config },
		ServiceConfigured: service != nil,
		Prober:            prober,
		Metadata:          metadata,
		Workers:           pools,
		Logger:            logger,
		Fetch:             adapter.prefetch,
		Sync:              adapter.syncDependency,
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

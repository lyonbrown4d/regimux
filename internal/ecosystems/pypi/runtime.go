package pypi

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
	"strings"
)

type runtimeAdapter struct {
	*depruntime.Adapter
	service *Service
}

func newRuntimeAdapter(service *Service, prober *ecosystem.EndpointProber, metadata meta.Store, pools *worker.Pools, logger *slog.Logger) *runtimeAdapter {
	adapter := &runtimeAdapter{service: service}
	options := depruntime.AdapterOptions[Upstream]{
		Name:              ecosystemPyPI,
		Label:             "pypi",
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

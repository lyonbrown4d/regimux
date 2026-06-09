// Package depruntime contains shared runtime glue for dependency ecosystems.
package depruntime

import (
	"context"
	"io"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/depprefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/samber/oops"
)

type ScheduledRuntime interface {
	ecosystem.Prefetcher
	ecosystem.Prober
}

func Upstreams[T any](ecosystemName string, upstreams *collectionlist.List[T], alias func(T) string, upstreamConfig func(T) config.UpstreamConfig) *collectionlist.List[ecosystem.Upstream] {
	if upstreams == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	return collectionlist.MapList(upstreams, func(_ int, upstream T) ecosystem.Upstream {
		return ecosystem.Upstream{
			Ecosystem: ecosystemName,
			Alias:     alias(upstream),
			Config:    upstreamConfig(upstream),
		}
	})
}

func Jobs(runtime ScheduledRuntime, serviceConfigured bool, prefetch config.SchedulerPrefetchConfig, refresh config.SchedulerManifestRefreshConfig) *collectionlist.List[ecosystem.JobSpec] {
	jobs := ecosystem.ProbeJobSpecs(runtime)
	if !serviceConfigured {
		return jobs
	}
	jobs.Add(ecosystem.PrefetchJobSpec(runtime, prefetch))
	jobs.Add(ecosystem.ManifestRefreshJobSpec(runtime, refresh, prefetch))
	return jobs
}

func ManualSyncCapability(name, label string, configured bool, upstreams *collectionlist.List[ecosystem.Upstream]) ecosystem.Capability {
	if !configured {
		return ecosystem.DisabledCapability(label+" proxy manual sync service is not configured", upstreams)
	}
	return ecosystem.EnabledCapability(label+" proxy manual sync is enabled", ecosystem.CapabilityTargets(upstreams))
}

func RunPrefetch(ctx context.Context, name, label string, prefetcher *depprefetch.Service, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if prefetcher == nil {
		return nil, oops.In(name).Errorf("%s proxy prefetcher is not configured", label)
	}
	report, err := prefetcher.Prefetch(ctx, opts)
	if err != nil {
		return report, oops.Wrapf(err, "prefetch %s proxy artifacts", label)
	}
	return report, nil
}

func CreateSyncJob(ctx context.Context, name, label string, service *manualsync.Service, opts manualsync.SyncOptions) (manualsync.SyncJob, error) {
	if service == nil {
		return manualsync.SyncJob{}, oops.In(name).Errorf("%s proxy manual sync service is not configured", label)
	}
	opts.Ecosystem = name
	job, err := service.CreateSyncJob(ctx, opts)
	if err != nil {
		return manualsync.SyncJob{}, oops.Wrapf(err, "create %s proxy manual sync job", label)
	}
	return job, nil
}

func RunSyncJob(ctx context.Context, name, label string, service *manualsync.Service, id string) error {
	if service == nil {
		return oops.In(name).Errorf("%s proxy manual sync service is not configured", label)
	}
	if err := service.RunSyncJob(ctx, id); err != nil {
		return oops.With("job_id", id).Wrapf(err, "run %s proxy manual sync job", label)
	}
	return nil
}

func SyncJob(service *manualsync.Service, id string) (manualsync.SyncJob, bool) {
	if service == nil {
		return manualsync.SyncJob{}, false
	}
	return service.SyncJob(id)
}

func MarkSyncJobFailed(service *manualsync.Service, id string, err error) {
	if service == nil {
		return
	}
	service.MarkSyncJobFailed(id, err)
}

func PrefetchResult(label, cache string, size int64, body io.Reader) (depprefetch.FetchResult, error) {
	if cache != artifactcache.CacheMiss {
		return depprefetch.FetchResult{}, nil
	}
	if body != nil {
		if _, err := io.Copy(io.Discard, body); err != nil {
			return depprefetch.FetchResult{}, oops.Wrapf(err, "drain %s prefetch response", label)
		}
	}
	return depprefetch.FetchResult{BytesWarmed: size}, nil
}

func DrainSuccessful(name, requestLabel, drainLabel string, status int, body io.Reader) (int64, error) {
	if status < 200 || status >= 300 {
		return 0, oops.In(name).With("status", status).Errorf("%s request failed", requestLabel)
	}
	if body == nil {
		return 0, nil
	}
	bytesWarmed, err := io.Copy(io.Discard, body)
	if err != nil {
		return 0, oops.With("status", status).Wrapf(err, "drain %s response", drainLabel)
	}
	return bytesWarmed, nil
}

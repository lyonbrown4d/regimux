package depruntime

import (
	"context"
	"log/slog"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/depprefetch"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/samber/oops"
)

type AdapterOptions[T any] struct {
	Name                string
	Label               string
	Upstreams           func() *collectionlist.List[T]
	CapabilityUpstreams func() *collectionlist.List[ecosystem.Upstream]
	UpstreamAlias       func(T) string
	UpstreamConfig      func(T) config.UpstreamConfig
	ServiceConfigured   bool
	PrefetchSchedule    config.SchedulerPrefetchConfig
	RefreshSchedule     config.SchedulerManifestRefreshConfig
	Prober              *ecosystem.EndpointProber
	Metadata            depprefetch.MetadataStore
	Workers             *worker.Pools
	Logger              *slog.Logger
	Fetch               func(context.Context, depprefetch.Candidate) (depprefetch.FetchResult, error)
	Sync                func(context.Context, manualsync.SyncOptions) (*manualsync.SyncReport, error)
}

type Adapter struct {
	name                string
	label               string
	upstreams           func() *collectionlist.List[ecosystem.Upstream]
	capabilityUpstreams func() *collectionlist.List[ecosystem.Upstream]
	serviceConfigured   bool
	prefetchSchedule    config.SchedulerPrefetchConfig
	refreshSchedule     config.SchedulerManifestRefreshConfig
	prober              *ecosystem.EndpointProber
	prefetcher          *depprefetch.Service
	manualSync          *manualsync.Service
}

func NewAdapter[T any](opts AdapterOptions[T]) *Adapter {
	var upstreams func() *collectionlist.List[ecosystem.Upstream]
	if opts.Upstreams != nil && opts.UpstreamAlias != nil && opts.UpstreamConfig != nil {
		upstreams = func() *collectionlist.List[ecosystem.Upstream] {
			return Upstreams(opts.Name, opts.Upstreams(), opts.UpstreamAlias, opts.UpstreamConfig)
		}
	}

	var prefetcher *depprefetch.Service
	if opts.Fetch != nil {
		prefetcher = depprefetch.New(depprefetch.Dependencies{
			Ecosystem: opts.Name,
			Metadata:  opts.Metadata,
			Workers:   opts.Workers,
			Logger:    opts.Logger,
			Fetch:     opts.Fetch,
		})
	}

	var manualSync *manualsync.Service
	if opts.Sync != nil {
		manualSync = manualsync.NewService(manualsync.ServiceDependencies{
			Execute: opts.Sync,
		})
	}

	return &Adapter{
		name:                opts.Name,
		label:               opts.Label,
		upstreams:           upstreams,
		capabilityUpstreams: opts.CapabilityUpstreams,
		serviceConfigured:   opts.ServiceConfigured,
		prefetchSchedule:    opts.PrefetchSchedule,
		refreshSchedule:     opts.RefreshSchedule,
		prober:              opts.Prober,
		prefetcher:          prefetcher,
		manualSync:          manualSync,
	}
}

func (a *Adapter) Name() string {
	if a == nil {
		return ""
	}
	return a.name
}

func (a *Adapter) Upstreams() *collectionlist.List[ecosystem.Upstream] {
	if a == nil || a.upstreams == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	return a.upstreams()
}

func (a *Adapter) UpstreamAliases() *collectionlist.List[string] {
	return ecosystem.UpstreamAliases(a.capabilityTargets())
}

func (a *Adapter) Jobs() *collectionlist.List[ecosystem.JobSpec] {
	if a == nil {
		return collectionlist.NewList[ecosystem.JobSpec]()
	}
	return Jobs(a, a.serviceConfigured, a.prefetchSchedule, a.refreshSchedule)
}

func (a *Adapter) ProbeCapability() ecosystem.Capability {
	return ecosystem.ProbeCapability(a.Upstreams())
}

func (a *Adapter) PrefetchCapability() ecosystem.Capability {
	return depprefetch.Capability(a.Name(), a.capabilityTargets())
}

func (a *Adapter) ManualSyncCapability() ecosystem.Capability {
	if a == nil {
		return ecosystem.DisabledCapability("dependency manual sync service is not configured", a.Upstreams())
	}
	return ManualSyncCapability(a.name, a.label, a.manualSync != nil, a.capabilityTargets())
}

func (a *Adapter) ProbeTargets() *collectionlist.List[ecosystem.ProbeTarget] {
	return ecosystem.ProbeTargets(a.Upstreams())
}

func (a *Adapter) Prefetch(ctx context.Context, opts ecosystem.PrefetchOptions) (*ecosystem.PrefetchReport, error) {
	if a == nil {
		return nil, oops.In("dependency").Errorf("dependency prefetcher is not configured")
	}
	return RunPrefetch(ctx, a.name, a.label, a.prefetcher, opts)
}

func (a *Adapter) Probe(ctx context.Context, target ecosystem.ProbeTarget) error {
	if a == nil {
		return oops.In("dependency").Errorf("dependency endpoint prober is not configured")
	}
	if a.prober == nil {
		return oops.In(a.name).Errorf("%s proxy endpoint prober is not configured", a.label)
	}
	if err := a.prober.Probe(ctx, target); err != nil {
		return oops.Wrapf(err, "probe %s proxy upstream", a.label)
	}
	return nil
}

func (a *Adapter) CreateSyncJob(ctx context.Context, opts manualsync.SyncOptions) (manualsync.SyncJob, error) {
	if a == nil {
		return manualsync.SyncJob{}, oops.In("dependency").Errorf("dependency manual sync service is not configured")
	}
	return CreateSyncJob(ctx, a.name, a.label, a.manualSync, opts)
}

func (a *Adapter) RunSyncJob(ctx context.Context, id string) error {
	if a == nil {
		return oops.In("dependency").Errorf("dependency manual sync service is not configured")
	}
	return RunSyncJob(ctx, a.name, a.label, a.manualSync, id)
}

func (a *Adapter) MarkSyncJobFailed(id string, err error) {
	if a == nil {
		return
	}
	MarkSyncJobFailed(a.manualSync, id, err)
}

func (a *Adapter) SyncJob(id string) (manualsync.SyncJob, bool) {
	if a == nil {
		return manualsync.SyncJob{}, false
	}
	return SyncJob(a.manualSync, id)
}

func (a *Adapter) capabilityTargets() *collectionlist.List[ecosystem.Upstream] {
	if a == nil {
		return collectionlist.NewList[ecosystem.Upstream]()
	}
	if a.capabilityUpstreams != nil {
		targets := a.capabilityUpstreams()
		if targets != nil {
			return targets
		}
	}
	return a.Upstreams()
}

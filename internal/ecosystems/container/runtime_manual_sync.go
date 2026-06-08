package container

import (
	"context"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	containerreference "github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/manualsync"
	"github.com/samber/oops"
)

func (r *Runtime) ManualSyncCapability() ecosystem.Capability {
	if r == nil || r.manual == nil {
		return ecosystem.DisabledCapability("container manual sync service is not configured", r.Upstreams())
	}
	return ecosystem.EnabledCapability("container manual sync is enabled", ecosystem.CapabilityTargets(r.Upstreams()))
}

func (r *Runtime) CreateSyncJob(ctx context.Context, opts manualsync.SyncOptions) (manualsync.SyncJob, error) {
	if r == nil || r.manual == nil {
		return manualsync.SyncJob{}, oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container manual sync service is not configured")
	}
	opts, err := r.normalizeSyncOptions(opts)
	if err != nil {
		return manualsync.SyncJob{}, err
	}
	job, err := r.manual.CreateSyncJob(ctx, opts)
	if err != nil {
		return manualsync.SyncJob{}, oops.Wrapf(err, "create container manual sync job")
	}
	return job, nil
}

func (r *Runtime) normalizeSyncOptions(opts manualsync.SyncOptions) (manualsync.SyncOptions, error) {
	alias := strings.TrimSpace(opts.Alias)
	artifact := strings.Trim(strings.TrimSpace(opts.Artifact), "/")
	ref := strings.TrimSpace(opts.Reference)
	if ref == "" {
		ref = "latest"
	}
	route, err := containerreference.ParseManifestPath("/v2/" + alias + "/" + artifact + "/manifests/" + ref)
	if err != nil {
		return manualsync.SyncOptions{}, oops.In("container").Wrapf(err, "normalize container manual sync target")
	}
	upstreamCfg, ok := r.cfg.ContainerUpstream(route.Alias)
	if !ok {
		return manualsync.SyncOptions{}, oops.In("container").With("alias", route.Alias).Errorf("unknown upstream alias %q", route.Alias)
	}
	normalized := route.WithDefaultNamespace(upstreamCfg.DefaultNamespace)
	opts.Ecosystem = ecosystem.Container
	opts.Alias = normalized.Alias
	opts.Artifact = normalized.Repo
	opts.Reference = normalized.Reference
	if opts.Accept == "" {
		opts.Accept = r.cfg.Scheduler.Prefetch.Accept
	}
	return opts, nil
}

func (r *Runtime) RunSyncJob(ctx context.Context, id string) error {
	if r == nil || r.manual == nil {
		return oops.In("container").With("ecosystem", ecosystem.Container).Errorf("container manual sync service is not configured")
	}
	if err := r.manual.RunSyncJob(ctx, id); err != nil {
		return oops.With("job_id", id).Wrapf(err, "run container manual sync job")
	}
	return nil
}

func (r *Runtime) MarkSyncJobFailed(id string, err error) {
	if r == nil || r.manual == nil {
		return
	}
	r.manual.MarkSyncJobFailed(id, err)
}

func (r *Runtime) SyncJob(id string) (manualsync.SyncJob, bool) {
	if r == nil || r.manual == nil {
		return manualsync.SyncJob{}, false
	}
	return r.manual.SyncJob(id)
}

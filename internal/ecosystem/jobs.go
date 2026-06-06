package ecosystem

import (
	"context"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/oops"
)

func ProbeJobSpecs(prober Prober) *collectionlist.List[JobSpec] {
	jobs := collectionlist.NewList[JobSpec]()
	if prober == nil {
		return jobs
	}
	prober.ProbeTargets().Range(func(_ int, target ProbeTarget) bool {
		jobTarget := target
		jobs.Add(JobSpec{
			Name:                  "regimux." + jobTarget.Ecosystem + ".probe." + jobTarget.Alias,
			Kind:                  JobProbe,
			Ecosystem:             jobTarget.Ecosystem,
			Alias:                 jobTarget.Alias,
			Tags:                  collectionlist.NewList("maintenance", "probe", jobTarget.Ecosystem, jobTarget.Alias),
			Interval:              jobTarget.Config.Probe.Interval,
			Enabled:               true,
			Distributed:           false,
			StartImmediately:      true,
			ProbeJitter:           jobTarget.Config.Probe.Jitter,
			ObserveEndpointHealth: true,
			Run: func(ctx context.Context) (JobRunResult, error) {
				if err := prober.Probe(ctx, jobTarget); err != nil {
					return JobRunResult{}, oops.With("ecosystem", jobTarget.Ecosystem, "alias", jobTarget.Alias).Wrapf(err, "run probe job")
				}
				return JobRunResult{}, nil
			},
		})
		return true
	})
	return jobs
}

func PrefetchJobSpec(prefetcher Prefetcher, cfg config.SchedulerPrefetchConfig) JobSpec {
	return JobSpec{
		Name:        "regimux." + prefetcher.Name() + ".prefetch",
		Kind:        JobPrefetch,
		Ecosystem:   prefetcher.Name(),
		Tags:        collectionlist.NewList("maintenance", "prefetch", prefetcher.Name()),
		Interval:    cfg.Interval,
		Enabled:     cfg.Enabled && cfg.Interval > 0,
		Distributed: cfg.Distributed,
		Run: func(ctx context.Context) (JobRunResult, error) {
			report, err := prefetcher.Prefetch(ctx, PrefetchOptionsFromConfig(cfg, false))
			if err != nil {
				return JobRunResult{PrefetchReport: report}, oops.With("ecosystem", prefetcher.Name()).Wrapf(err, "run prefetch job")
			}
			return JobRunResult{PrefetchReport: report}, nil
		},
	}
}

func ManifestRefreshJobSpec(prefetcher Prefetcher, refresh config.SchedulerManifestRefreshConfig, prefetch config.SchedulerPrefetchConfig) JobSpec {
	cfg := refresh.EffectiveFor(prefetcher.Name())
	return JobSpec{
		Name:        "regimux." + prefetcher.Name() + ".manifest_refresh",
		Kind:        JobManifestRefresh,
		Ecosystem:   prefetcher.Name(),
		Tags:        collectionlist.NewList("maintenance", "manifest-refresh", prefetcher.Name()),
		Interval:    cfg.Interval,
		Enabled:     cfg.Enabled && cfg.Interval > 0,
		Distributed: cfg.Distributed,
		Run: func(ctx context.Context) (JobRunResult, error) {
			report, err := prefetcher.Prefetch(ctx, PrefetchOptionsFromConfig(prefetch, true))
			if err != nil {
				return JobRunResult{PrefetchReport: report}, oops.With("ecosystem", prefetcher.Name()).Wrapf(err, "run manifest refresh job")
			}
			return JobRunResult{PrefetchReport: report}, nil
		},
	}
}

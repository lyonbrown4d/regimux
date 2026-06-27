package ecosystem

import (
	"context"
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/lo"
)

// PrefetchReport summarizes a prefetch run for any ecosystem.
type PrefetchReport struct {
	Ecosystem           string
	ScannedRecords      int
	SkippedRecords      int
	Repositories        int
	SkippedRepositories int
	Candidates          int
	Prefetched          int
	Failed              int
	SkippedCandidates   int
	BytesWarmed         int64
	RetryRequested      bool
	Canceled            bool
}

// Prefetcher is implemented by ecosystems that can warm artifacts in the background.
type Prefetcher interface {
	Runtime
	Prefetch(context.Context, PrefetchOptions) (*PrefetchReport, error)
}

// UnsupportedCapability marks a capability as intentionally unavailable.
func UnsupportedCapability(reason string, upstreams *collectionlist.List[Upstream]) Capability {
	return Capability{
		State:   CapabilityUnsupported,
		Reason:  reason,
		Targets: CapabilityTargets(upstreams),
	}
}

// DisabledCapability marks a capability as implemented but not enabled.
func DisabledCapability(reason string, upstreams *collectionlist.List[Upstream]) Capability {
	return Capability{
		State:   CapabilityDisabled,
		Reason:  reason,
		Targets: CapabilityTargets(upstreams),
	}
}

// EnabledCapability marks a capability as implemented and schedulable.
func EnabledCapability(reason string, targets *collectionlist.List[CapabilityTarget]) Capability {
	if targets == nil {
		targets = collectionlist.NewList[CapabilityTarget]()
	}
	return Capability{
		State:   CapabilityEnabled,
		Reason:  reason,
		Targets: targets,
	}
}

// ProbeCapability reports whether an upstream set has any enabled probe targets.
func ProbeCapability(upstreams *collectionlist.List[Upstream]) Capability {
	targets := CapabilityTargetsFromProbeTargets(ProbeTargets(upstreams))
	if targets.Len() == 0 {
		return DisabledCapability("probe is not enabled for any upstream", upstreams)
	}
	return EnabledCapability("probe is enabled", targets)
}

// ProbeTargets returns enabled probe targets from upstream snapshots.
func ProbeTargets(upstreams *collectionlist.List[Upstream]) *collectionlist.List[ProbeTarget] {
	if upstreams == nil {
		return collectionlist.NewList[ProbeTarget]()
	}
	targets := lo.FilterMap(upstreams.Values(), func(upstream Upstream, _ int) (ProbeTarget, bool) {
		probeCfg := upstream.Config.Probe
		return ProbeTarget(upstream), probeCfg.Enabled && probeCfg.Interval > 0
	})
	return collectionlist.NewList(targets...)
}

// CapabilityTargets converts upstream snapshots to capability targets.
func CapabilityTargets(upstreams *collectionlist.List[Upstream]) *collectionlist.List[CapabilityTarget] {
	if upstreams == nil {
		return collectionlist.NewList[CapabilityTarget]()
	}
	return collectionlist.NewList(lo.Map(upstreams.Values(), func(upstream Upstream, _ int) CapabilityTarget {
		return CapabilityTarget(upstream)
	})...)
}

// CapabilityTargetsFromProbeTargets converts probe targets to capability metadata.
func CapabilityTargetsFromProbeTargets(probes *collectionlist.List[ProbeTarget]) *collectionlist.List[CapabilityTarget] {
	if probes == nil {
		return collectionlist.NewList[CapabilityTarget]()
	}
	return collectionlist.NewList(lo.Map(probes.Values(), func(target ProbeTarget, _ int) CapabilityTarget {
		return CapabilityTarget(target)
	})...)
}

// UpstreamAliases returns aliases from upstream snapshots in their configured order.
func UpstreamAliases(upstreams *collectionlist.List[Upstream]) *collectionlist.List[string] {
	if upstreams == nil {
		return collectionlist.NewList[string]()
	}
	return collectionlist.NewList(lo.Map(upstreams.Values(), func(upstream Upstream, _ int) string {
		return upstream.Alias
	})...)
}

// ConfiguredUpstreams returns all upstreams exposed by runtime providers.
func ConfiguredUpstreams(runtimes *collectionlist.List[Runtime]) *collectionlist.List[Upstream] {
	if runtimes == nil {
		return collectionlist.NewList[Upstream]()
	}
	return collectionlist.NewList(lo.FlatMap(runtimes.Values(), configuredRuntimeUpstreams)...)
}

func configuredRuntimeUpstreams(runtime Runtime, _ int) []Upstream {
	if runtime == nil {
		return nil
	}
	provider, ok := runtime.(UpstreamProvider)
	if !ok || provider == nil {
		return nil
	}
	name := strings.TrimSpace(runtime.Name())
	upstreams := provider.Upstreams()
	if upstreams == nil {
		return nil
	}
	return lo.FilterMap(upstreams.Values(), func(upstream Upstream, _ int) (Upstream, bool) {
		upstream.Ecosystem = upstreamEcosystem(name, upstream.Ecosystem)
		return upstream, upstream.Ecosystem != "" && strings.TrimSpace(upstream.Alias) != ""
	})
}

func upstreamEcosystem(runtimeName, upstreamName string) string {
	upstreamName = strings.TrimSpace(upstreamName)
	if upstreamName != "" {
		return upstreamName
	}
	return strings.TrimSpace(runtimeName)
}

// PrefetchOptionsFromConfig maps scheduler configuration to prefetch execution options.
func PrefetchOptionsFromConfig(cfg config.SchedulerPrefetchConfig, manifestOnly bool) PrefetchOptions {
	return PrefetchOptions{
		MaxRecords:           cfg.MaxRecords,
		MinPullCount:         cfg.MinPullCount,
		TagsPageSize:         cfg.TagsPageSize,
		MaxCandidatesPerRepo: cfg.MaxCandidatesPerRepo,
		MaxVersionDistance:   cfg.MaxVersionDistance,
		MaxBytes:             cfg.MaxBytes,
		MaxTasks:             cfg.MaxTasks,
		MaxRepositories:      cfg.MaxRepositories,
		FailureBackoff:       cfg.FailureBackoff,
		RetryWindow:          cfg.RetryWindow,
		Accept:               cfg.Accept,
		ManifestOnly:         manifestOnly,
	}
}

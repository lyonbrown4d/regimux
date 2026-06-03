// Package ecosystem defines shared runtime capabilities for package ecosystems.
package ecosystem

import (
	"context"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

const (
	Container = "container"
	Go        = "go"
	Maven     = "maven"
	NPM       = "npm"
	PyPI      = "pypi"
)

// Runtime identifies one supported dependency ecosystem.
type Runtime interface {
	Name() string
}

// Upstream describes one upstream alias within an ecosystem.
type Upstream struct {
	Ecosystem string
	Alias     string
	Config    config.UpstreamConfig
}

// UpstreamProvider exposes configured upstream aliases.
type UpstreamProvider interface {
	Runtime
	Upstreams() *collectionlist.List[Upstream]
}

// UpstreamAliasProvider exposes configured upstream aliases.
type UpstreamAliasProvider interface {
	Runtime
	UpstreamAliases() *collectionlist.List[string]
}

// CapabilityState describes whether a runtime capability can be scheduled.
type CapabilityState string

const (
	// CapabilityUnsupported means the runtime does not implement the capability yet.
	CapabilityUnsupported CapabilityState = "unsupported"
	// CapabilityDisabled means the runtime implements the capability but it is not enabled.
	CapabilityDisabled CapabilityState = "disabled"
	// CapabilityEnabled means the runtime capability is available for scheduling.
	CapabilityEnabled CapabilityState = "enabled"
)

// CapabilityTarget identifies one upstream target for capability metadata.
type CapabilityTarget struct {
	Ecosystem string
	Alias     string
	Config    config.UpstreamConfig
}

// Capability describes the scheduler-facing state of probe or prefetch support.
type Capability struct {
	State   CapabilityState
	Reason  string
	Targets *collectionlist.List[CapabilityTarget]
}

// CapabilityProvider exposes probe and prefetch support metadata.
type CapabilityProvider interface {
	Runtime
	ProbeCapability() Capability
	PrefetchCapability() Capability
}

// ProbeTarget identifies one scheduled upstream probe.
type ProbeTarget struct {
	Ecosystem string
	Alias     string
	Config    config.UpstreamConfig
}

// Prober is implemented by ecosystems that can actively probe upstreams.
type Prober interface {
	Runtime
	ProbeTargets() *collectionlist.List[ProbeTarget]
	Probe(context.Context, ProbeTarget) error
}

// EndpointHealthFlusher persists buffered endpoint health records.
type EndpointHealthFlusher interface {
	Runtime
	FlushEndpointHealth(context.Context) error
}

// PrefetchOptions carries scheduler-level prefetch limits.
type PrefetchOptions struct {
	MaxRecords           int
	MinPullCount         int64
	TagsPageSize         int
	MaxCandidatesPerRepo int
	MaxVersionDistance   int
	Accept               string
	MaxBytes             int64
	MaxTasks             int
	MaxRepositories      int
	FailureBackoff       time.Duration
	RetryWindow          time.Duration
	Now                  time.Time
}

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
	return collectionlist.FilterMapList(upstreams, func(_ int, upstream Upstream) (ProbeTarget, bool) {
		probeCfg := upstream.Config.Probe
		if !probeCfg.Enabled || probeCfg.Interval <= 0 {
			return ProbeTarget{}, false
		}
		return ProbeTarget(upstream), true
	})
}

// CapabilityTargets converts upstream snapshots to capability targets.
func CapabilityTargets(upstreams *collectionlist.List[Upstream]) *collectionlist.List[CapabilityTarget] {
	if upstreams == nil {
		return collectionlist.NewList[CapabilityTarget]()
	}
	return collectionlist.MapList(upstreams, func(_ int, upstream Upstream) CapabilityTarget {
		return CapabilityTarget(upstream)
	})
}

// CapabilityTargetsFromProbeTargets converts probe targets to capability metadata.
func CapabilityTargetsFromProbeTargets(probes *collectionlist.List[ProbeTarget]) *collectionlist.List[CapabilityTarget] {
	if probes == nil {
		return collectionlist.NewList[CapabilityTarget]()
	}
	return collectionlist.MapList(probes, func(_ int, target ProbeTarget) CapabilityTarget {
		return CapabilityTarget(target)
	})
}

// UpstreamAliases returns aliases from upstream snapshots in their configured order.
func UpstreamAliases(upstreams *collectionlist.List[Upstream]) *collectionlist.List[string] {
	if upstreams == nil {
		return collectionlist.NewList[string]()
	}
	return collectionlist.MapList(upstreams, func(_ int, upstream Upstream) string {
		return upstream.Alias
	})
}

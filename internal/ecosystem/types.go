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

// ClientSnapshot reports all upstream health snapshots for an ecosystem.
type ClientSnapshot struct {
	Upstreams *collectionlist.List[UpstreamSnapshot]
}

// UpstreamSnapshot reports runtime endpoint health and configuration hints for one upstream alias.
type UpstreamSnapshot struct {
	Ecosystem  string
	Alias      string
	Policy     string
	BlobPolicy string
	Endpoints  *collectionlist.List[EndpointSnapshot]
}

// EndpointSnapshot reports one endpoint instance under an upstream alias.
type EndpointSnapshot struct {
	Registry string
	Role     string
	Health   EndpointHealthSnapshot
}

// EndpointHealthSnapshot mirrors upstream endpoint health metrics for cross-ecosystem reporting.
type EndpointHealthSnapshot struct {
	Registry             string
	LatencyEWMA          time.Duration
	LatencySamples       int
	HasLatency           bool
	ConsecutiveFailures  int
	CooldownUntil        time.Time
	DegradedUntil        time.Time
	Inflight             int
	LastSuccessAt        time.Time
	LastFailureAt        time.Time
	LastProbeAt          time.Time
	SuccessCount         int64
	FailureCount         int64
	ContentMismatchCount int64
	HasSuccessRate       bool
	SuccessRate          float64
	Score                time.Duration
	InCooldown           bool
	InDegraded           bool
}

type JobKind string

const (
	JobCleanup             JobKind = "cleanup"
	JobProbe               JobKind = "probe"
	JobPrefetch            JobKind = "prefetch"
	JobManifestRefresh     JobKind = "manifest_refresh"
	JobEndpointHealthFlush JobKind = "endpoint_health_flush"
	JobManualSync          JobKind = "manual_sync"
)

type JobSpec struct {
	Name                  string
	Kind                  JobKind
	Ecosystem             string
	Alias                 string
	Tags                  *collectionlist.List[string]
	Interval              time.Duration
	Enabled               bool
	Distributed           bool
	StartImmediately      bool
	ProbeJitter           time.Duration
	ObserveEndpointHealth bool
	Run                   func(context.Context) (JobRunResult, error)
}

type JobRunResult struct {
	PrefetchReport *PrefetchReport
	CleanupReport  *CleanupReport
}

type JobProvider interface {
	Runtime
	Jobs() *collectionlist.List[JobSpec]
}

// UpstreamSnapshotProvider exposes health snapshot of upstreams managed by the runtime.
type UpstreamSnapshotProvider interface {
	Runtime
	Snapshot(time.Time) ClientSnapshot
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
	ManifestOnly         bool
	MaxBytes             int64
	MaxTasks             int
	MaxRepositories      int
	FailureBackoff       time.Duration
	RetryWindow          time.Duration
	Now                  time.Time
}

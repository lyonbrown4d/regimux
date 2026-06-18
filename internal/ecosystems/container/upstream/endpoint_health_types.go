package upstream

import (
	"strings"
	"sync"
	"time"

	collectionmapping "github.com/arcgolabs/collectionx/mapping"
)

const (
	defaultEndpointHealthAlpha                   = 0.2
	defaultEndpointHealthFailurePenalty          = 500 * time.Millisecond
	defaultEndpointHealthInflightPenalty         = 50 * time.Millisecond
	defaultEndpointHealthCooldown                = 2 * time.Minute
	defaultEndpointHealthUnknownLatency          = time.Second
	defaultEndpointHealthFailureThreshold        = 1
	defaultEndpointHealthContentMismatchCooldown = 5 * time.Minute
)

type EndpointHealthOptions struct {
	Alpha                   float64
	FailurePenalty          time.Duration
	InflightPenalty         time.Duration
	Cooldown                time.Duration
	UnknownLatency          time.Duration
	FailureThreshold        int
	ContentMismatchCooldown time.Duration
}

type EndpointHealthTracker struct {
	mu     sync.Mutex
	opts   EndpointHealthOptions
	states *collectionmapping.Table[string, string, *endpointHealthState]
}

type endpointHealthState struct {
	registry             string
	repository           string
	latencyEWMA          time.Duration
	latencySamples       int
	consecutiveFailures  int
	cooldownUntil        time.Time
	degradedUntil        time.Time
	inflight             int
	lastSuccessAt        time.Time
	lastFailureAt        time.Time
	lastProbeAt          time.Time
	successCount         int64
	failureCount         int64
	contentMismatchCount int64
}

type EndpointHealthSnapshot struct {
	Registry             string
	Repository           string
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

type EndpointHealthCandidate struct {
	Registry string
	State    EndpointHealthSnapshot
}

type endpointHealthCandidateRank struct {
	candidate EndpointHealthCandidate
	index     int
}

type endpointRuntimeCandidate struct {
	runtime upstreamRuntime
	state   EndpointHealthSnapshot
	index   int
}

func NewEndpointHealthTracker(opts EndpointHealthOptions) *EndpointHealthTracker {
	return &EndpointHealthTracker{
		opts:   normalizeEndpointHealthOptions(opts),
		states: collectionmapping.NewTable[string, string, *endpointHealthState](),
	}
}

func normalizeEndpointHealthOptions(opts EndpointHealthOptions) EndpointHealthOptions {
	if opts.Alpha <= 0 || opts.Alpha > 1 {
		opts.Alpha = defaultEndpointHealthAlpha
	}
	if opts.FailurePenalty <= 0 {
		opts.FailurePenalty = defaultEndpointHealthFailurePenalty
	}
	if opts.InflightPenalty <= 0 {
		opts.InflightPenalty = defaultEndpointHealthInflightPenalty
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = defaultEndpointHealthCooldown
	}
	if opts.UnknownLatency <= 0 {
		opts.UnknownLatency = defaultEndpointHealthUnknownLatency
	}
	if opts.FailureThreshold <= 0 {
		opts.FailureThreshold = defaultEndpointHealthFailureThreshold
	}
	if opts.ContentMismatchCooldown <= 0 {
		opts.ContentMismatchCooldown = defaultEndpointHealthContentMismatchCooldown
	}
	return opts
}

func normalizeEndpointHealthRegistry(registry string) string {
	return strings.TrimRight(strings.TrimSpace(registry), "/")
}

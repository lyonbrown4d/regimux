package upstream

import "time"

func (p *upstreamPool) recordProbeSuccess(runtime upstreamRuntime, latency time.Duration, now time.Time) EndpointHealthSnapshot {
	if p == nil {
		return EndpointHealthSnapshot{}
	}
	return p.health.RecordProbeSuccess(runtime.config.Registry, latency, now)
}

func (p *upstreamPool) recordProbeFailure(runtime upstreamRuntime, now time.Time) EndpointHealthSnapshot {
	if p == nil {
		return EndpointHealthSnapshot{}
	}
	return p.health.RecordProbeFailure(runtime.config.Registry, now)
}

func (p *upstreamPool) recordRequestSuccess(runtime upstreamRuntime, repository string, now time.Time) EndpointHealthSnapshot {
	if p == nil {
		return EndpointHealthSnapshot{}
	}
	return p.health.RecordRequestSuccess(runtime.config.Registry, repository, now)
}

func (p *upstreamPool) recordRequestFailure(runtime upstreamRuntime, repository string, now time.Time) EndpointHealthSnapshot {
	if p == nil {
		return EndpointHealthSnapshot{}
	}
	return p.health.RecordRequestFailure(runtime.config.Registry, repository, now)
}

func (p *upstreamPool) recordContentInconsistent(runtime upstreamRuntime, repository string, now time.Time) EndpointHealthSnapshot {
	if p == nil {
		return EndpointHealthSnapshot{}
	}
	return p.health.RecordContentInconsistent(runtime.config.Registry, repository, now)
}

func (p *upstreamPool) probeEnabled() bool {
	return p != nil && p.probeConfig.Enabled
}

func (p *upstreamPool) blobAttemptConcurrency() int {
	if p == nil || p.blobMaxAttempts <= 0 {
		return 1
	}
	return p.blobMaxAttempts
}

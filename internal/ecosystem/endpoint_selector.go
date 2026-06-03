package ecosystem

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const (
	endpointUnknownLatency = time.Second
	endpointFailurePenalty = 500 * time.Millisecond
)

type endpointHealthCandidate struct {
	endpoint    string
	score       time.Duration
	inCooldown  bool
	inDegraded  bool
	originalIdx int
}

func UpstreamEndpoints(
	ctx context.Context,
	metadata meta.EndpointHealthRepository,
	ecosystem, alias string,
	cfg config.UpstreamConfig,
) []string {
	endpoints := normalizedUpstreamEndpoints(cfg)
	if len(endpoints) == 0 {
		return nil
	}
	if metadata == nil {
		return endpoints
	}

	records, err := metadata.ListEndpointHealth(ctx, meta.EndpointHealthListAlias(ScopedAlias(ecosystem, alias)))
	if err != nil || len(records) == 0 {
		return endpoints
	}

	recordByEndpoint := make(map[string]meta.EndpointHealthRecord, len(records))
	for _, record := range records {
		registry := normalizeEndpoint(record.Registry)
		if registry == "" {
			continue
		}
		// Keep the latest record for each endpoint; records are already sorted by
		// UpdatedAt/ID descending, so the first one is the freshest.
		if existing, ok := recordByEndpoint[registry]; !ok || existing.UpdatedAt.Before(record.UpdatedAt) {
			recordByEndpoint[registry] = record
		}
	}

	candidates := make([]endpointHealthCandidate, 0, len(endpoints))
	now := time.Now()
	for i, endpoint := range endpoints {
		record, ok := recordByEndpoint[endpoint]
		if !ok {
			candidates = append(candidates, endpointHealthCandidate{
				endpoint:    endpoint,
				score:       endpointUnknownLatency,
				originalIdx: i,
			})
			continue
		}

		inCooldown := !record.CooldownUntil.IsZero() && now.Before(record.CooldownUntil)
		inDegraded := !record.DegradedUntil.IsZero() && now.Before(record.DegradedUntil)
		score := endpointUnknownLatency
		if record.LatencySamples > 0 {
			score = record.LatencyEWMA
		}
		score += time.Duration(record.ConsecutiveFailures) * endpointFailurePenalty
		if inDegraded {
			score += endpointFailurePenalty
		}
		candidates = append(candidates, endpointHealthCandidate{
			endpoint:    endpoint,
			score:       score,
			inCooldown:  inCooldown,
			inDegraded:  inDegraded,
			originalIdx: i,
		})
	}

	if len(candidates) <= 1 {
		out := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			out = append(out, candidate.endpoint)
		}
		return out
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.inCooldown != right.inCooldown {
			return !left.inCooldown
		}
		if left.inDegraded != right.inDegraded {
			return !left.inDegraded
		}
		if left.score != right.score {
			return left.score < right.score
		}
		return left.originalIdx < right.originalIdx
	})

	healthy := make([]endpointHealthCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.inCooldown || candidate.inDegraded {
			continue
		}
		healthy = append(healthy, candidate)
	}
	if len(healthy) == 0 {
		healthy = candidates
	}

	out := make([]string, 0, len(healthy))
	for _, candidate := range healthy {
		out = append(out, candidate.endpoint)
	}
	return out
}

func normalizedUpstreamEndpoints(cfg config.UpstreamConfig) []string {
	out := make([]string, 0, 1+len(cfg.Mirrors))
	if cfg.Registry != "" {
		if registry := normalizeEndpoint(cfg.Registry); registry != "" {
			out = append(out, registry)
		}
	}
	for _, mirror := range cfg.Mirrors {
		if mirror = normalizeEndpoint(mirror); mirror != "" {
			out = append(out, mirror)
		}
	}
	return out
}

func normalizeEndpoint(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

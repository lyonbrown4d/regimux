package ecosystem

import (
	"context"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
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

	recordByEndpoint := collectionmapping.NewMapWithCapacity[string, meta.EndpointHealthRecord](len(records))
	for _, record := range records {
		registry := normalizeEndpoint(record.Registry)
		if registry == "" {
			continue
		}
		// Keep the latest record for each endpoint; records are already sorted by
		// UpdatedAt/ID descending, so the first one is the freshest.
		existing, ok := recordByEndpoint.Get(registry)
		if !ok || existing.UpdatedAt.Before(record.UpdatedAt) {
			recordByEndpoint.Set(registry, record)
		}
	}

	now := time.Now()
	candidates := collectionlist.FilterMapList(collectionlist.NewList(endpoints...), func(i int, endpoint string) (endpointHealthCandidate, bool) {
		record, hasRecord := recordByEndpoint.Get(endpoint)
		if !hasRecord {
			return endpointHealthCandidate{
				endpoint:    endpoint,
				score:       endpointUnknownLatency,
				originalIdx: i,
			}, true
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
		return endpointHealthCandidate{
			endpoint:    endpoint,
			score:       score,
			inCooldown:  inCooldown,
			inDegraded:  inDegraded,
			originalIdx: i,
		}, true
	})

	if candidates.Len() <= 1 {
		return collectionlist.MapList(candidates, func(_ int, candidate endpointHealthCandidate) string {
			return candidate.endpoint
		}).Values()
	}

	candidates = candidates.Sort(compareEndpointHealthCandidate)
	healthy := collectionlist.FilterList(candidates, func(_ int, candidate endpointHealthCandidate) bool {
		return !candidate.inCooldown && !candidate.inDegraded
	})
	if healthy.Len() == 0 {
		healthy = candidates
	}

	return collectionlist.MapList(healthy, func(_ int, candidate endpointHealthCandidate) string {
		return candidate.endpoint
	}).Values()
}

func compareEndpointHealthCandidate(left, right endpointHealthCandidate) int {
	if left.inCooldown != right.inCooldown {
		if left.inCooldown {
			return 1
		}
		return -1
	}
	if left.inDegraded != right.inDegraded {
		if left.inDegraded {
			return 1
		}
		return -1
	}
	if left.score != right.score {
		if left.score < right.score {
			return -1
		}
		return 1
	}
	if left.originalIdx < right.originalIdx {
		return -1
	}
	if left.originalIdx > right.originalIdx {
		return 1
	}
	return 0
}

func normalizedUpstreamEndpoints(cfg config.UpstreamConfig) []string {
	return collectionlist.FilterMapList(
		collectionlist.NewList(append([]string{cfg.Registry}, cfg.Mirrors...)...),
		func(_ int, endpoint string) (string, bool) {
			if endpoint = normalizeEndpoint(endpoint); endpoint != "" {
				return endpoint, true
			}
			return "", false
		},
	).Values()
}

func normalizeEndpoint(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

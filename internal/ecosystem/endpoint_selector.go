package ecosystem

import (
	"cmp"
	"context"
	"strings"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/lo"
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

	recordByEndpoint := latestEndpointHealthByEndpoint(ctx, metadata, ecosystem, alias)
	if recordByEndpoint == nil {
		return endpoints
	}
	candidates := endpointHealthCandidates(endpoints, recordByEndpoint)
	return mapEndpointCandidates(prioritizeEndpointCandidates(candidates))
}

func latestEndpointHealthByEndpoint(ctx context.Context, metadata meta.EndpointHealthRepository, ecosystem, alias string) *collectionmapping.Map[string, meta.EndpointHealthRecord] {
	records, err := metadata.ListEndpointHealth(ctx, meta.EndpointHealthListAlias(ScopedAlias(ecosystem, alias)))
	if err != nil || records.Len() == 0 {
		return nil
	}

	recordByEndpoint := collectionmapping.NewMapWithCapacity[string, meta.EndpointHealthRecord](records.Len())
	foundAny := false
	recordValues := records.Values()
	for i := range recordValues {
		record := recordValues[i]
		registry := normalizeEndpoint(record.Registry)
		if registry == "" {
			continue
		}
		existing, ok := recordByEndpoint.Get(registry)
		if !ok || existing.UpdatedAt.Before(record.UpdatedAt) {
			recordByEndpoint.Set(registry, record)
			foundAny = true
		}
	}
	if !foundAny {
		return nil
	}
	return recordByEndpoint
}

func endpointHealthCandidates(endpoints []string, recordByEndpoint *collectionmapping.Map[string, meta.EndpointHealthRecord]) *collectionlist.List[endpointHealthCandidate] {
	now := time.Now()
	return collectionlist.NewList(lo.Map(endpoints, func(endpoint string, index int) endpointHealthCandidate {
		return buildEndpointHealthCandidate(endpoint, index, now, recordByEndpoint)
	})...)
}

func buildEndpointHealthCandidate(
	endpoint string,
	originalIdx int,
	now time.Time,
	recordByEndpoint *collectionmapping.Map[string, meta.EndpointHealthRecord],
) endpointHealthCandidate {
	record, hasRecord := recordByEndpoint.Get(endpoint)
	if !hasRecord {
		return endpointHealthCandidate{
			endpoint:    endpoint,
			score:       endpointUnknownLatency,
			originalIdx: originalIdx,
		}
	}
	candidate := endpointHealthCandidate{
		endpoint:    endpoint,
		originalIdx: originalIdx,
		score:       record.LatencyEWMA,
		inCooldown:  !record.CooldownUntil.IsZero() && now.Before(record.CooldownUntil),
		inDegraded:  !record.DegradedUntil.IsZero() && now.Before(record.DegradedUntil),
	}
	candidate.score += time.Duration(record.ConsecutiveFailures) * endpointFailurePenalty
	if candidate.inDegraded {
		candidate.score += endpointFailurePenalty
	}
	return candidate
}

func prioritizeEndpointCandidates(candidates *collectionlist.List[endpointHealthCandidate]) *collectionlist.List[endpointHealthCandidate] {
	if candidates == nil || candidates.Len() <= 1 {
		return candidates
	}

	candidates = candidates.Sort(compareEndpointHealthCandidate)
	healthy := collectionlist.NewList(lo.Filter(candidates.Values(), func(candidate endpointHealthCandidate, _ int) bool {
		return !candidate.inCooldown && !candidate.inDegraded
	})...)
	if healthy.Len() == 0 {
		return candidates
	}
	return healthy
}

func mapEndpointCandidates(candidates *collectionlist.List[endpointHealthCandidate]) []string {
	if candidates == nil {
		return nil
	}
	return lo.Map(candidates.Values(), func(candidate endpointHealthCandidate, _ int) string {
		return candidate.endpoint
	})
}

func compareEndpointHealthCandidate(left, right endpointHealthCandidate) int {
	if penaltyOrder := compareInts(endpointHealthPenalty(left), endpointHealthPenalty(right)); penaltyOrder != 0 {
		return penaltyOrder
	}
	if scoreOrder := compareDurations(left.score, right.score); scoreOrder != 0 {
		return scoreOrder
	}
	return compareInts(left.originalIdx, right.originalIdx)
}

func endpointHealthPenalty(candidate endpointHealthCandidate) int {
	penalty := 0
	if candidate.inCooldown {
		penalty += 2
	}
	if candidate.inDegraded {
		penalty++
	}
	return penalty
}

func compareInts(left, right int) int {
	return cmp.Compare(left, right)
}

func compareDurations(left, right time.Duration) int {
	return cmp.Compare(left, right)
}

func normalizedUpstreamEndpoints(cfg config.UpstreamConfig) []string {
	ordered := append([]string{cfg.Registry}, cfg.Mirrors...)
	return lo.FilterMap(ordered, func(endpoint string, _ int) (string, bool) {
		endpoint = normalizeEndpoint(endpoint)
		return endpoint, endpoint != ""
	})
}

func normalizeEndpoint(endpoint string) string {
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}

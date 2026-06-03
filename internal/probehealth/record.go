package probehealth

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/samber/oops"
)

const (
	unknownLatencyPenalty = time.Second
	failurePenalty        = 500 * time.Millisecond
	mismatchPenalty       = 5 * time.Second
	cooldownPenalty       = time.Hour
	degradedPenalty       = time.Minute
	maxPenaltyCount       = 1000

	fieldKey                  = "key"
	fieldAlias                = "alias"
	fieldRegistry             = "registry"
	fieldRepository           = "repository"
	fieldLatencyEWMA          = "latency_ewma"
	fieldLatencySamples       = "latency_samples"
	fieldConsecutiveFailures  = "consecutive_failures"
	fieldSuccessCount         = "success_count"
	fieldFailureCount         = "failure_count"
	fieldContentMismatchCount = "content_mismatch_count"
	fieldCooldownUntil        = "cooldown_until"
	fieldDegradedUntil        = "degraded_until"
	fieldLastSuccessAt        = "last_success_at"
	fieldLastFailureAt        = "last_failure_at"
	fieldLastProbeAt          = "last_probe_at"
	fieldCreatedAt            = "created_at"
	fieldUpdatedAt            = "updated_at"
)

func marshalRecord(record meta.EndpointHealthRecord) map[string]any {
	return map[string]any{
		fieldKey:                  record.Key,
		fieldAlias:                record.Alias,
		fieldRegistry:             record.Registry,
		fieldRepository:           record.Repository,
		fieldLatencyEWMA:          int64(record.LatencyEWMA),
		fieldLatencySamples:       record.LatencySamples,
		fieldConsecutiveFailures:  record.ConsecutiveFailures,
		fieldSuccessCount:         record.SuccessCount,
		fieldFailureCount:         record.FailureCount,
		fieldContentMismatchCount: record.ContentMismatchCount,
		fieldCooldownUntil:        unixNano(record.CooldownUntil),
		fieldDegradedUntil:        unixNano(record.DegradedUntil),
		fieldLastSuccessAt:        unixNano(record.LastSuccessAt),
		fieldLastFailureAt:        unixNano(record.LastFailureAt),
		fieldLastProbeAt:          unixNano(record.LastProbeAt),
		fieldCreatedAt:            unixNano(record.CreatedAt),
		fieldUpdatedAt:            unixNano(record.UpdatedAt),
	}
}

func unmarshalRecord(fields map[string]string) (meta.EndpointHealthRecord, error) {
	record := meta.EndpointHealthRecord{
		Key:                  strings.TrimSpace(fields[fieldKey]),
		Alias:                strings.TrimSpace(fields[fieldAlias]),
		Registry:             normalizeRegistry(fields[fieldRegistry]),
		Repository:           normalizeRepository(fields[fieldRepository]),
		LatencyEWMA:          time.Duration(parseInt64(fields[fieldLatencyEWMA])),
		LatencySamples:       int(parseInt64(fields[fieldLatencySamples])),
		ConsecutiveFailures:  int(parseInt64(fields[fieldConsecutiveFailures])),
		SuccessCount:         parseInt64(fields[fieldSuccessCount]),
		FailureCount:         parseInt64(fields[fieldFailureCount]),
		ContentMismatchCount: parseInt64(fields[fieldContentMismatchCount]),
		CooldownUntil:        unixTime(parseInt64(fields[fieldCooldownUntil])),
		DegradedUntil:        unixTime(parseInt64(fields[fieldDegradedUntil])),
		LastSuccessAt:        unixTime(parseInt64(fields[fieldLastSuccessAt])),
		LastFailureAt:        unixTime(parseInt64(fields[fieldLastFailureAt])),
		LastProbeAt:          unixTime(parseInt64(fields[fieldLastProbeAt])),
		CreatedAt:            unixTime(parseInt64(fields[fieldCreatedAt])),
		UpdatedAt:            unixTime(parseInt64(fields[fieldUpdatedAt])),
	}
	if record.Key == "" {
		record.Key = endpointHealthKey(record)
	}
	if record.Alias == "" || record.Registry == "" {
		return meta.EndpointHealthRecord{}, oops.In("probehealth").Errorf("probe health hot state is missing alias or registry")
	}
	return record, nil
}

func endpointHealthKey(record meta.EndpointHealthRecord) string {
	alias := strings.TrimSpace(record.Alias)
	registry := normalizeRegistry(record.Registry)
	if alias == "" || registry == "" {
		return ""
	}
	return meta.EndpointHealthKey{
		Alias:      alias,
		Registry:   registry,
		Repository: normalizeRepository(record.Repository),
	}.String()
}

func endpointHealthScore(record meta.EndpointHealthRecord, now time.Time) float64 {
	score := unknownLatencyPenalty
	if record.LatencySamples > 0 && record.LatencyEWMA > 0 {
		score = record.LatencyEWMA
	}
	score += boundedPenalty(int64(record.ConsecutiveFailures), failurePenalty)
	score += boundedPenalty(record.ContentMismatchCount, mismatchPenalty)
	if record.CooldownUntil.After(now) {
		score += cooldownPenalty + record.CooldownUntil.Sub(now)
	}
	if record.DegradedUntil.After(now) {
		score += degradedPenalty
	}
	if score < 0 {
		return 0
	}
	return float64(score.Nanoseconds())
}

func boundedPenalty(count int64, unit time.Duration) time.Duration {
	if count <= 0 {
		return 0
	}
	if count > maxPenaltyCount {
		count = maxPenaltyCount
	}
	return time.Duration(count) * unit
}

func normalizeRegistry(registry string) string {
	return strings.TrimRight(strings.TrimSpace(registry), "/")
}

func normalizeRepository(repository string) string {
	return strings.Trim(strings.TrimSpace(repository), "/")
}

func normalizePrefix(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ":")
	if prefix == "" {
		return defaultPrefix
	}
	return prefix
}

func encodedToken(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func parseInt64(value string) int64 {
	if value == "" {
		return 0
	}
	out, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return out
}

func unixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixNano()
}

func unixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}

package ecosystem_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	collectionset "github.com/arcgolabs/collectionx/set"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestUpstreamEndpointsUsesHealthForRouting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux-selector-health.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", store.Close()) })

	cfg := config.UpstreamConfig{
		Registry: "https://registry.internal/",
		Mirrors:  []string{"https://mirror-better.internal/", "https://mirror-cooldown.internal/"},
	}

	alias := ecosystem.ScopedAlias(ecosystem.Go, "default")
	now := time.Now().UTC()

	writeEndpointHealth(ctx, t, store, alias, "https://registry.internal", meta.EndpointHealthRecord{
		LatencyEWMA:         400 * time.Millisecond,
		LatencySamples:      3,
		SuccessCount:        5,
		FailureCount:        1,
		LastProbeAt:         now,
		CreatedAt:           now.Add(-time.Minute),
		UpdatedAt:           now.Add(-time.Minute),
		ConsecutiveFailures: 1,
	})
	writeEndpointHealth(ctx, t, store, alias, "https://mirror-better.internal", meta.EndpointHealthRecord{
		LatencyEWMA:    50 * time.Millisecond,
		LatencySamples: 3,
		SuccessCount:   3,
		LastProbeAt:    now,
		CreatedAt:      now.Add(-time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	})
	writeEndpointHealth(ctx, t, store, alias, "https://mirror-cooldown.internal", meta.EndpointHealthRecord{
		LatencyEWMA:         10 * time.Millisecond,
		LatencySamples:      3,
		DegradedUntil:       now.Add(10 * time.Minute),
		FailureCount:        1,
		ConsecutiveFailures: 1,
		CreatedAt:           now.Add(-time.Minute),
		UpdatedAt:           now.Add(-time.Minute),
	})

	endpoints := ecosystem.UpstreamEndpoints(ctx, store, ecosystem.Go, "default", cfg)
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 selectable healthy endpoints, got %d (%v)", len(endpoints), endpoints)
	}
	if endpoints[0] != "https://mirror-better.internal" {
		t.Fatalf("expected fastest healthy endpoint first, got %v", endpoints)
	}
	if endpoints[1] != "https://registry.internal" {
		t.Fatalf("expected fallback primary endpoint second, got %v", endpoints)
	}
}

func TestUpstreamEndpointsFallsBackWhenAllEndpointsAreUnhealthy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux-selector-unhealthy.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", store.Close()) })

	cfg := config.UpstreamConfig{
		Registry: "https://primary.internal/",
		Mirrors:  []string{"https://mirror-a.internal/", "https://mirror-b.internal/"},
	}

	alias := ecosystem.ScopedAlias(ecosystem.Go, "default")
	now := time.Now().UTC()

	writeEndpointHealth(ctx, t, store, alias, "https://primary.internal", meta.EndpointHealthRecord{
		LatencyEWMA:   100 * time.Millisecond,
		CooldownUntil: now.Add(10 * time.Minute),
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now.Add(-time.Minute),
	})
	writeEndpointHealth(ctx, t, store, alias, "https://mirror-a.internal", meta.EndpointHealthRecord{
		LatencyEWMA:   20 * time.Millisecond,
		CooldownUntil: now.Add(10 * time.Minute),
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now.Add(-time.Minute),
	})
	writeEndpointHealth(ctx, t, store, alias, "https://mirror-b.internal", meta.EndpointHealthRecord{
		LatencyEWMA:   30 * time.Millisecond,
		CooldownUntil: now.Add(10 * time.Minute),
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now.Add(-time.Minute),
	})

	endpoints := ecosystem.UpstreamEndpoints(ctx, store, ecosystem.Go, "default", cfg)
	if got := len(endpoints); got != 3 {
		t.Fatalf("expected fallback to all endpoints when all unhealthy, got %d (%v)", got, endpoints)
	}
	expected := collectionset.NewSet[string]("https://primary.internal", "https://mirror-a.internal", "https://mirror-b.internal")
	for _, endpoint := range endpoints {
		expected.Remove(endpoint)
	}
	if !expected.IsEmpty() {
		t.Fatalf("expected fallback to include all configured endpoints, got %v", endpoints)
	}
}

func TestUpstreamEndpointsUsesLatestRecordPerEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux-selector-latest.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", store.Close()) })

	cfg := config.UpstreamConfig{
		Registry: "https://primary.internal/",
		Mirrors:  []string{"https://fallback.internal/"},
	}

	alias := ecosystem.ScopedAlias(ecosystem.Go, "latest")
	// Insert an older degraded latency sample for the primary endpoint.
	writeEndpointHealthWithRepository(ctx, t, store, alias, "https://primary.internal", "older-repo", meta.EndpointHealthRecord{
		LatencyEWMA:    500 * time.Millisecond,
		LatencySamples: 1,
		FailureCount:   1,
	})
	time.Sleep(20 * time.Millisecond)
	// Insert a later healthier sample for the same endpoint under a different repository.
	writeEndpointHealthWithRepository(ctx, t, store, alias, "https://primary.internal", "newer-repo", meta.EndpointHealthRecord{
		LatencyEWMA:    10 * time.Millisecond,
		LatencySamples: 2,
		SuccessCount:   2,
	})
	// Make sure the fallback endpoint remains a bit slower than the fresh primary sample.
	writeEndpointHealthWithRepository(ctx, t, store, alias, "https://fallback.internal", "f-repo", meta.EndpointHealthRecord{
		LatencyEWMA:    100 * time.Millisecond,
		LatencySamples: 2,
		SuccessCount:   2,
	})

	endpoints := ecosystem.UpstreamEndpoints(ctx, store, ecosystem.Go, "latest", cfg)
	if len(endpoints) != 2 {
		t.Fatalf("expected 2 selectable endpoints, got %d (%v)", len(endpoints), endpoints)
	}
	if endpoints[0] != "https://primary.internal" {
		t.Fatalf("expected latest primary health record to win, got %v", endpoints)
	}
}

func TestUpstreamEndpointsIsScopedByEcosystemAndAlias(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux-selector-scope.db")})
	requireNoError(t, "open metadata store", err)
	t.Cleanup(func() { requireNoError(t, "close metadata store", store.Close()) })

	cfg := config.UpstreamConfig{
		Registry: "https://go-primary.internal/",
		Mirrors:  []string{"https://go-mirror.internal/"},
	}

	now := time.Now().UTC()
	// Add a fast healthy record in Maven for the same mirror used by Go.
	writeEndpointHealth(ctx, t, store, ecosystem.ScopedAlias(ecosystem.Maven, "default"), "https://go-mirror.internal", meta.EndpointHealthRecord{
		LatencyEWMA: 5 * time.Millisecond,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	// Go has only unhealthy records; if cross-ecosystem records leaked, mirror would be incorrectly preferred.
	writeEndpointHealth(ctx, t, store, ecosystem.ScopedAlias(ecosystem.Go, "default"), "https://go-primary.internal", meta.EndpointHealthRecord{
		LatencyEWMA:   100 * time.Millisecond,
		CooldownUntil: now.Add(10 * time.Minute),
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	writeEndpointHealth(ctx, t, store, ecosystem.ScopedAlias(ecosystem.Go, "default"), "https://go-mirror.internal", meta.EndpointHealthRecord{
		LatencyEWMA:   200 * time.Millisecond,
		CooldownUntil: now.Add(10 * time.Minute),
		CreatedAt:     now,
		UpdatedAt:     now,
	})

	endpoints := ecosystem.UpstreamEndpoints(ctx, store, ecosystem.Go, "default", cfg)
	if got := len(endpoints); got != 2 {
		t.Fatalf("expected all go endpoints in fallback mode, got %d (%v)", got, endpoints)
	}
	// Both endpoints are in cooldown for Go, so original order must be preserved.
	if endpoints[0] != "https://go-primary.internal" || endpoints[1] != "https://go-mirror.internal" {
		t.Fatalf("expected go endpoint order by configured order, got %v", endpoints)
	}
}

func writeEndpointHealth(ctx context.Context, t *testing.T, store meta.Store, alias, registry string, record meta.EndpointHealthRecord) {
	t.Helper()
	record.Alias = alias
	record.Registry = registry
	if _, err := store.UpsertEndpointHealth(ctx, record); err != nil {
		t.Fatalf("upsert endpoint health (registry=%s): %v", registry, err)
	}
}

func writeEndpointHealthWithRepository(
	ctx context.Context,
	t *testing.T,
	store meta.Store,
	alias, registry, repository string,
	record meta.EndpointHealthRecord,
) {
	t.Helper()
	record.Alias = alias
	record.Registry = registry
	record.Repository = repository
	if _, err := store.UpsertEndpointHealth(ctx, record); err != nil {
		t.Fatalf("upsert endpoint health (registry=%s repository=%s): %v", registry, repository, err)
	}
}

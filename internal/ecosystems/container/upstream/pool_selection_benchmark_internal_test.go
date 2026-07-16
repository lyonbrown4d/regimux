package upstream

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
)

const benchmarkRepository = "library/alpine"

func TestSelectHealthyRuntimesReusesHomogeneousInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	pool, runtimes := benchmarkOrderedPool(4)
	if selected := pool.selectHealthyRuntimes(context.Background(), runtimes, benchmarkRepository, now, "manifest"); selected != runtimes {
		t.Fatal("all-healthy selection copied the runtime list")
	}

	runtimes.Range(func(_ int, runtime upstreamRuntime) bool {
		markBenchmarkRuntimeUnhealthy(pool, runtime.config.Registry, now)
		return true
	})
	if selected := pool.selectHealthyRuntimes(context.Background(), runtimes, benchmarkRepository, now, "manifest"); selected != runtimes {
		t.Fatal("all-unhealthy fallback copied the runtime list")
	}
}

func TestSelectHealthyRuntimesAllocatesOnlyForMixedHealth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	pool, runtimes := benchmarkOrderedPool(4)
	unhealthy, _ := runtimes.Get(1)
	markBenchmarkRuntimeUnhealthy(pool, unhealthy.config.Registry, now)

	selected := pool.selectHealthyRuntimes(context.Background(), runtimes, benchmarkRepository, now, "manifest")
	if selected == runtimes {
		t.Fatal("mixed-health selection reused the unfiltered runtime list")
	}
	if selected.Len() != runtimes.Len()-1 {
		t.Fatalf("selected runtimes = %d, want %d", selected.Len(), runtimes.Len()-1)
	}
	selected.Range(func(_ int, runtime upstreamRuntime) bool {
		if runtime.config.Registry == unhealthy.config.Registry {
			t.Errorf("selected unhealthy registry %q", runtime.config.Registry)
		}
		return true
	})
}

func BenchmarkSelectHealthyRuntimesOrderedAllHealthy(b *testing.B) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	pool, runtimes := benchmarkOrderedPool(16)
	pool.selectHealthyRuntimes(context.Background(), runtimes, benchmarkRepository, now, "manifest")

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		selected := pool.selectHealthyRuntimes(context.Background(),
			runtimes,
			benchmarkRepository,
			now,
			"manifest",
		)
		if selected != runtimes {
			b.Fatal("all-healthy selection copied the runtime list")
		}
	}
}

func BenchmarkSelectHealthyRuntimesOrderedMixed(b *testing.B) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	pool, runtimes := benchmarkOrderedPool(16)
	unhealthy, _ := runtimes.Get(7)
	markBenchmarkRuntimeUnhealthy(pool, unhealthy.config.Registry, now)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		selected := pool.selectHealthyRuntimes(context.Background(),
			runtimes,
			benchmarkRepository,
			now,
			"manifest",
		)
		if selected.Len() != 15 {
			b.Fatalf("selected runtimes = %d, want 15", selected.Len())
		}
	}
}

func BenchmarkLimiterCacheHit(b *testing.B) {
	pool, runtimes := benchmarkOrderedPool(16)
	runtime, _ := runtimes.Get(0)
	cached := pool.limiter(runtime.config.Registry)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if limiter := pool.limiter(runtime.config.Registry); limiter != cached {
			b.Fatal("limiter cache returned a different instance")
		}
	}
}

func benchmarkOrderedPool(
	count int,
) (*upstreamPool, *collectionlist.List[upstreamRuntime]) {
	runtimes := collectionlist.NewListWithCapacity[upstreamRuntime](count)
	for index := range count {
		runtimes.Add(upstreamRuntime{config: Config{
			Registry: fmt.Sprintf("https://mirror-%d.example.test", index),
		}})
	}
	pool := newUpstreamPool(Config{
		Alias:        "hub",
		MirrorPolicy: mirrorPolicyOrdered,
		Blob: BlobConfig{
			MirrorPolicy:              mirrorPolicyOrdered,
			MaxConcurrencyPerEndpoint: 4,
		},
	}, slog.New(slog.DiscardHandler), runtimes)
	return pool, runtimes
}

func markBenchmarkRuntimeUnhealthy(
	pool *upstreamPool,
	registry string,
	now time.Time,
) {
	pool.health.RestoreSnapshot(EndpointHealthSnapshot{
		Registry:      registry,
		CooldownUntil: now.Add(time.Minute),
	})
}

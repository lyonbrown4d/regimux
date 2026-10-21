package backend

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryCacheDefaultsNonpositiveItemLimit(t *testing.T) {
	if got := memoryMaxItems(0); got != defaultMemoryMaxItems {
		t.Fatalf("zero item limit = %d, want bounded default %d", got, defaultMemoryMaxItems)
	}
	if got := memoryMaxItems(-1); got != defaultMemoryMaxItems {
		t.Fatalf("negative item limit = %d, want bounded default %d", got, defaultMemoryMaxItems)
	}
	if got := memoryMaxItems(17); got != 17 {
		t.Fatalf("explicit item limit = %d, want 17", got)
	}
}

func TestMemoryCacheDefaultsNonpositiveByteLimit(t *testing.T) {
	if got := memoryMaxBytes(0); got != defaultMemoryMaxBytes {
		t.Fatalf("zero byte limit = %d, want bounded default %d", got, defaultMemoryMaxBytes)
	}
	if got := memoryMaxBytes(-1); got != defaultMemoryMaxBytes {
		t.Fatalf("negative byte limit = %d, want bounded default %d", got, defaultMemoryMaxBytes)
	}
	if got := memoryMaxBytes(17); got != 17 {
		t.Fatalf("explicit byte limit = %d, want 17", got)
	}
}

func TestMemoryByteBudgetEvictsByKeyAndValueSize(t *testing.T) {
	cache := NewMemory(MemoryOptions{MaxItems: 10, MaxBytes: 11})
	ctx := context.Background()
	setBudgetValue(ctx, t, cache, "a", "1234")
	setBudgetValue(ctx, t, cache, "b", "1234")
	assertMemoryBudgetWithinLimit(t, cache)
	setBudgetValue(ctx, t, cache, "c", "123456")
	assertMemoryBudgetWithinLimit(t, cache)
	assertMemoryCacheHit(ctx, t, cache, "a", false, "oldest entry")
	assertMemoryCacheHit(ctx, t, cache, "b", false, "next oldest entry")
	assertMemoryCacheHit(ctx, t, cache, "c", true, "newest entry")
}

func setBudgetValue(ctx context.Context, t *testing.T, cache *Memory, key, value string) {
	t.Helper()
	if err := cache.Set(ctx, key, []byte(value), time.Minute); err != nil {
		t.Fatal(err)
	}
}

func assertMemoryBudgetWithinLimit(t *testing.T, cache *Memory) {
	t.Helper()
	if cache.currentBytes > cache.maxBytes {
		t.Fatalf("cached bytes = %d, exceeds max %d", cache.currentBytes, cache.maxBytes)
	}
}

func assertMemoryCacheHit(ctx context.Context, t *testing.T, cache *Memory, key string, want bool, label string) {
	t.Helper()
	if _, ok, err := cache.Get(ctx, key); err != nil || ok != want {
		t.Fatalf("%s hit=%v err=%v, want hit=%v", label, ok, err, want)
	}
}

func TestMemoryByteBudgetReplacementAndOversizedBypass(t *testing.T) {
	cache := NewMemory(MemoryOptions{MaxItems: 10, MaxBytes: 16})
	ctx := context.Background()

	if err := cache.Set(ctx, "key", []byte("old"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(ctx, "key", []byte("replacement"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if cache.currentBytes != int64(len("key")+len("replacement")) {
		t.Fatalf("cached bytes after replacement = %d", cache.currentBytes)
	}
	if err := cache.Set(ctx, "key", []byte("this value exceeds the cache budget"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if cache.currentBytes != 0 {
		t.Fatalf("cached bytes after oversized bypass = %d, want 0", cache.currentBytes)
	}
	if _, ok, err := cache.Get(ctx, "key"); err != nil || ok {
		t.Fatalf("stale value survived oversized bypass: hit=%v err=%v", ok, err)
	}
}

func TestMemoryByteBudgetExpiryDeleteAndPurgeAccounting(t *testing.T) {
	cache := NewMemory(MemoryOptions{MaxItems: 10, MaxBytes: 64})
	ctx := context.Background()
	setBudgetValueWithTTL(ctx, t, cache, "expire", "value", time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	assertMemoryCacheHit(ctx, t, cache, "expire", false, "expired entry")
	assertMemoryByteCount(t, cache, 0, "expiry")
	deleteMemoryValue(ctx, t, cache, "delete")
	assertMemoryByteCount(t, cache, 0, "delete")
	setBudgetValueWithTTL(ctx, t, cache, "tag:one", "tag", 0)
	setBudgetValueWithTTL(ctx, t, cache, "blob:one", "blob", 0)
	if err := cache.DeletePrefix(ctx, "tag:"); err != nil {
		t.Fatal(err)
	}
	assertMemoryByteCount(t, cache, int64(len("blob:one")+len("blob")), "prefix delete")
	setBudgetValueWithTTL(ctx, t, cache, "purge", "value", 0)
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	assertMemoryByteCount(t, cache, 0, "purge")
}

func setBudgetValueWithTTL(ctx context.Context, t *testing.T, cache *Memory, key, value string, ttl time.Duration) {
	t.Helper()
	if err := cache.Set(ctx, key, []byte(value), ttl); err != nil {
		t.Fatal(err)
	}
}

func deleteMemoryValue(ctx context.Context, t *testing.T, cache *Memory, key string) {
	t.Helper()
	setBudgetValueWithTTL(ctx, t, cache, key, "value", 0)
	if err := cache.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
}

func assertMemoryByteCount(t *testing.T, cache *Memory, want int64, operation string) {
	t.Helper()
	if cache.currentBytes != want {
		t.Fatalf("cached bytes after %s = %d, want %d", operation, cache.currentBytes, want)
	}
}

func TestMemoryByteBudgetConcurrentAccounting(t *testing.T) {
	cache := NewMemory(MemoryOptions{MaxItems: 32, MaxBytes: 256})
	ctx := context.Background()
	const workers = 32
	const writes = 100
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Go(func() {
			exerciseMemoryBudget(ctx, t, cache, worker, writes)
		})
	}
	wg.Wait()
	if cache.currentBytes < 0 || cache.currentBytes > cache.maxBytes {
		t.Fatalf("cached bytes after concurrent operations = %d, max=%d", cache.currentBytes, cache.maxBytes)
	}
}

func exerciseMemoryBudget(ctx context.Context, t *testing.T, cache *Memory, worker, writes int) {
	t.Helper()
	for range writes {
		key := fmt.Sprintf("worker-%d", worker)
		if err := cache.Set(ctx, key, []byte("0123456789"), time.Minute); err != nil {
			t.Errorf("set: %v", err)
			return
		}
		if _, _, err := cache.Get(ctx, key); err != nil {
			t.Errorf("get: %v", err)
			return
		}
	}
}

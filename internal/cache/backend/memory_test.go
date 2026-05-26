package backend_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

func TestMemorySetGetDelete(t *testing.T) {
	ctx := context.Background()
	cache := backend.NewMemory(backend.MemoryOptions{})
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close memory backend: %v", err)
		}
	})

	setErr := cache.Set(ctx, "manifest", []byte("body"), time.Minute)
	if setErr != nil {
		t.Fatalf("set value: %v", setErr)
	}

	got, ok, err := cache.Get(ctx, "manifest")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if !ok {
		t.Fatal("expected cache hit")
	}
	if !bytes.Equal(got, []byte("body")) {
		t.Fatalf("unexpected value %q", got)
	}

	deleteErr := cache.Delete(ctx, "manifest")
	if deleteErr != nil {
		t.Fatalf("delete value: %v", deleteErr)
	}

	_, ok, err = cache.Get(ctx, "manifest")
	if err != nil {
		t.Fatalf("get deleted value: %v", err)
	}
	if ok {
		t.Fatal("expected cache miss after delete")
	}
}

func TestMemoryTTL(t *testing.T) {
	ctx := context.Background()
	cache := backend.NewMemory(backend.MemoryOptions{})

	if err := cache.Set(ctx, "token", []byte("expired"), 20*time.Millisecond); err != nil {
		t.Fatalf("set value: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		_, ok, err := cache.Get(ctx, "token")
		if err != nil {
			t.Fatalf("get value: %v", err)
		}
		if !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected cache miss after ttl")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMemoryMaxItems(t *testing.T) {
	ctx := context.Background()
	cache := backend.NewMemory(backend.MemoryOptions{MaxItems: 1})

	if err := cache.Set(ctx, "first", []byte("1"), time.Minute); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := cache.Set(ctx, "second", []byte("2"), time.Minute); err != nil {
		t.Fatalf("set second: %v", err)
	}

	_, ok, err := cache.Get(ctx, "first")
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	if ok {
		t.Fatal("expected first item to be evicted")
	}

	got, ok, err := cache.Get(ctx, "second")
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if !ok || !bytes.Equal(got, []byte("2")) {
		t.Fatalf("unexpected second item hit=%v value=%q", ok, got)
	}
}

func TestMemoryCopiesValues(t *testing.T) {
	ctx := context.Background()
	cache := backend.NewMemory(backend.MemoryOptions{})

	value := []byte("abc")
	if err := cache.Set(ctx, "copy", value, time.Minute); err != nil {
		t.Fatalf("set value: %v", err)
	}
	value[0] = 'z'

	got, ok, err := cache.Get(ctx, "copy")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if !ok || !bytes.Equal(got, []byte("abc")) {
		t.Fatalf("unexpected stored value hit=%v value=%q", ok, got)
	}

	got[0] = 'z'
	gotAgain, ok, err := cache.Get(ctx, "copy")
	if err != nil {
		t.Fatalf("get value again: %v", err)
	}
	if !ok || !bytes.Equal(gotAgain, []byte("abc")) {
		t.Fatalf("unexpected stored value after returned slice mutation hit=%v value=%q", ok, gotAgain)
	}
}

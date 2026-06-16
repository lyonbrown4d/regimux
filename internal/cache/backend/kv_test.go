package backend_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/arcgolabs/kvx"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

func TestKVBackendPrefixAndNil(t *testing.T) {
	ctx := context.Background()
	client := &fakeKVClient{values: map[string][]byte{}}
	cache := backend.NewKV(client, "regimux")

	_, ok, err := cache.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("get missing value: %v", err)
	}
	if ok {
		t.Fatal("expected cache miss")
	}

	setErr := cache.Set(ctx, "manifest", []byte("body"), time.Minute)
	if setErr != nil {
		t.Fatalf("set value: %v", setErr)
	}
	_, hasPrefixedKey := client.values["regimux:manifest"]
	if !hasPrefixedKey {
		t.Fatal("expected prefixed key in kv client")
	}

	got, ok, err := cache.Get(ctx, "manifest")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if !ok || !bytes.Equal(got, []byte("body")) {
		t.Fatalf("unexpected hit=%v value=%q", ok, got)
	}
}

func TestKVBackendCopiesValues(t *testing.T) {
	ctx := context.Background()
	client := &fakeKVClient{values: map[string][]byte{}}
	cache := backend.NewKV(client, "")

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
}

func TestKVBackendAcquiresPrefixedLease(t *testing.T) {
	ctx := context.Background()
	client := &fakeKVClient{values: map[string][]byte{}}
	cache := backend.NewKV(client, "regimux")

	lease := requireLeaseAcquired(ctx, t, cache, "first")
	if _, held := client.locks["regimux:artifact-fill"]; !held {
		t.Fatal("expected prefixed lock key in kv client")
	}

	requireLeaseDenied(ctx, t, cache)

	extendErr := lease.Extend(ctx, time.Minute)
	if extendErr != nil {
		t.Fatalf("extend lease: %v", extendErr)
	}
	releaseErr := lease.Release(ctx)
	if releaseErr != nil {
		t.Fatalf("release lease: %v", releaseErr)
	}

	requireLeaseAcquired(ctx, t, cache, "third")
}

func requireLeaseAcquired(ctx context.Context, t *testing.T, cache *backend.KV, label string) backend.Lease {
	t.Helper()
	lease, ok, err := cache.AcquireLease(ctx, "artifact-fill", time.Minute)
	if err != nil {
		t.Fatalf("acquire %s lease: %v", label, err)
	}
	if !ok || lease == nil {
		t.Fatal("expected lease acquisition to succeed after release")
	}
	return lease
}

func requireLeaseDenied(ctx context.Context, t *testing.T, cache *backend.KV) {
	t.Helper()
	lease, ok, err := cache.AcquireLease(ctx, "artifact-fill", time.Minute)
	if err != nil {
		t.Fatalf("acquire second lease: %v", err)
	}
	if ok || lease != nil {
		t.Fatal("expected second lease acquisition to be denied while first is held")
	}
}

type fakeKVClient struct {
	mu     sync.Mutex
	values map[string][]byte
	locks  map[string]string
}

func (c *fakeKVClient) Get(_ context.Context, key string) ([]byte, error) {
	value, ok := c.values[key]
	if !ok {
		return nil, kvx.ErrNil
	}
	return value, nil
}

func (c *fakeKVClient) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	if key == "error" {
		return errors.New("set error")
	}
	c.values[key] = value
	return nil
}

func (c *fakeKVClient) Delete(_ context.Context, key string) error {
	delete(c.values, key)
	return nil
}

func (c *fakeKVClient) Close() error {
	return nil
}

func (c *fakeKVClient) Acquire(_ context.Context, key, token string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks == nil {
		c.locks = map[string]string{}
	}
	if _, ok := c.locks[key]; ok {
		return false, nil
	}
	c.locks[key] = token
	return true, nil
}

func (c *fakeKVClient) Release(_ context.Context, key, token string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks == nil || c.locks[key] != token {
		return false, nil
	}
	delete(c.locks, key)
	return true, nil
}

func (c *fakeKVClient) Extend(_ context.Context, key, token string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks == nil || c.locks[key] != token {
		return false, nil
	}
	return true, nil
}

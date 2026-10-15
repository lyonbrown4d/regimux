package backend_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

func TestMemoryDeletePrefixUsesLiteralNamespacedPrefix(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := backend.NewMemory(backend.MemoryOptions{Prefix: "tenant-a"})
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close memory backend: %v", err)
		}
	})

	for _, key := range []string{
		"manifest:repo:tag:application/json",
		"manifest:repo:tag:application/vnd.example+json",
		"manifest:repo:tag2:application/json",
		"manifest:repo:tag*:application/json",
		"manifest:repo:tagX:application/json",
		"other:manifest:repo:tag:application/json",
	} {
		if err := cache.Set(ctx, key, []byte(key), time.Minute); err != nil {
			t.Fatalf("set %q: %v", key, err)
		}
	}

	var deleter backend.PrefixDeleter = cache
	if err := deleter.DeletePrefix(ctx, "manifest:repo:tag:"); err != nil {
		t.Fatalf("delete prefix: %v", err)
	}

	for _, key := range []string{
		"manifest:repo:tag:application/json",
		"manifest:repo:tag:application/vnd.example+json",
	} {
		if _, found, err := cache.Get(ctx, key); err != nil || found {
			t.Fatalf("expected %q removed, found=%v err=%v", key, found, err)
		}
	}
	for _, key := range []string{
		"manifest:repo:tag2:application/json",
		"manifest:repo:tag*:application/json",
		"manifest:repo:tagX:application/json",
		"other:manifest:repo:tag:application/json",
	} {
		if _, found, err := cache.Get(ctx, key); err != nil || !found {
			t.Fatalf("expected %q preserved, found=%v err=%v", key, found, err)
		}
	}
}

func TestKVDeletePrefixAppliesNamespaceAndPropagatesErrors(t *testing.T) {
	t.Parallel()

	client := &prefixKVClient{fakeKVClient: &fakeKVClient{values: map[string][]byte{
		"tenant-a:manifest:repo:tag:json":  []byte("match"),
		"tenant-a:manifest:repo:tag2:json": []byte("neighbor"),
		"tenant-b:manifest:repo:tag:json":  []byte("other namespace"),
	}}}
	cache := backend.NewKV(client, "tenant-a")
	var deleter backend.PrefixDeleter = cache

	if err := deleter.DeletePrefix(context.Background(), "manifest:repo:tag:"); err != nil {
		t.Fatalf("delete prefix: %v", err)
	}
	if got := client.deletedPrefix; got != "tenant-a:manifest:repo:tag:" {
		t.Fatalf("DeletePrefix received %q, want namespaced exact prefix", got)
	}
	if _, found := client.values["tenant-a:manifest:repo:tag:json"]; found {
		t.Fatal("matching namespaced key remains")
	}
	if _, found := client.values["tenant-a:manifest:repo:tag2:json"]; !found {
		t.Fatal("adjacent reference was deleted")
	}
	if _, found := client.values["tenant-b:manifest:repo:tag:json"]; !found {
		t.Fatal("different namespace key was deleted")
	}

	client.prefixErr = errors.New("scan failure")
	err := deleter.DeletePrefix(context.Background(), "manifest:repo:tag:")
	if err == nil || !strings.Contains(err.Error(), "scan failure") {
		t.Fatalf("DeletePrefix error = %v, want scan failure", err)
	}
}

func TestDeletePrefixRejectsEmptyLogicalPrefix(t *testing.T) {
	t.Parallel()

	cache := backend.NewMemory(backend.MemoryOptions{Prefix: "tenant-a"})
	t.Cleanup(func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close memory backend: %v", err)
		}
	})
	var deleter backend.PrefixDeleter = cache

	for _, prefix := range []string{"", " : ", ":"} {
		if err := deleter.DeletePrefix(context.Background(), prefix); !errors.Is(err, backend.ErrEmptyKey) {
			t.Errorf("DeletePrefix(%q) error = %v, want ErrEmptyKey", prefix, err)
		}
	}
}

func TestNoopDeletePrefixValidatesLogicalPrefix(t *testing.T) {
	t.Parallel()

	var deleter backend.PrefixDeleter = backend.Noop{}
	if err := deleter.DeletePrefix(context.Background(), "manifest:"); err != nil {
		t.Fatalf("delete prefix from noop backend: %v", err)
	}
	if err := deleter.DeletePrefix(context.Background(), "::"); !errors.Is(err, backend.ErrEmptyKey) {
		t.Fatalf("empty noop prefix error = %v, want ErrEmptyKey", err)
	}
}

func TestKVDeletePrefixPreservesCancellation(t *testing.T) {
	t.Parallel()

	client := &prefixKVClient{fakeKVClient: &fakeKVClient{values: map[string][]byte{}}}
	cache := backend.NewKV(client, "tenant-a")
	var deleter backend.PrefixDeleter = cache
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := deleter.DeletePrefix(ctx, "manifest:repo:tag:")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DeletePrefix error = %v, want context.Canceled", err)
	}
}

type prefixKVClient struct {
	*fakeKVClient
	deletedPrefix string
	prefixErr     error
}

func (c *prefixKVClient) DeletePrefix(ctx context.Context, prefix string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.deletedPrefix = prefix
	if c.prefixErr != nil {
		return c.prefixErr
	}
	for key := range c.values {
		if strings.HasPrefix(key, prefix) {
			delete(c.values, key)
		}
	}
	return nil
}

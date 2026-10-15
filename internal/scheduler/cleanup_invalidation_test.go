package scheduler

import (
	"context"
	"errors"
	"testing"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
)

type invalidatingCleanupPlanner struct {
	testPackageCleanupPlanner
	name       string
	invalidate func(context.Context, ecosystem.CacheCleanupGroup) error
}

func (p invalidatingCleanupPlanner) Name() string { return p.name }

func (p invalidatingCleanupPlanner) InvalidateCacheCleanup(ctx context.Context, group ecosystem.CacheCleanupGroup) error {
	return p.invalidate(ctx, group)
}

func TestSharedCleanupDispatchesInvalidationBeforeDurableRetirement(t *testing.T) {
	runtime, metadata, objects := newSharedCleanupRuntime(t)
	digests := seedSharedCleanupArtifacts(t, metadata, objects)
	want := errors.New("cache invalidation failed")
	called := 0
	runtime.runtimes = collectionlist.NewList[ecosystem.Runtime](invalidatingCleanupPlanner{
		name: "test",
		invalidate: func(context.Context, ecosystem.CacheCleanupGroup) error {
			called++
			return want
		},
	})
	if err := runtime.runCleanup(t.Context()); !errors.Is(err, want) {
		t.Fatalf("cleanup error = %v, want %v", err, want)
	}
	if called != 1 {
		t.Fatalf("invalidation calls = %d", called)
	}
	for _, digest := range digests {
		if exists, err := objects.Exists(t.Context(), digest); err != nil || !exists {
			t.Fatalf("failed invalidation removed object %s: %v", digest, err)
		}
	}
}

func TestSharedCleanupDoesNotInvalidateOtherEcosystems(t *testing.T) {
	runtime, metadata, objects := newSharedCleanupRuntime(t)
	seedSharedCleanupArtifacts(t, metadata, objects)
	runtime.runtimes = collectionlist.NewList[ecosystem.Runtime](invalidatingCleanupPlanner{
		name: "another-ecosystem",
		invalidate: func(context.Context, ecosystem.CacheCleanupGroup) error {
			t.Fatal("unrelated ecosystem received invalidation")
			return nil
		},
	})
	if err := runtime.runCleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestOrphanCleanupDispatchesToEveryInvalidator(t *testing.T) {
	runtime, _, _ := newSharedCleanupRuntime(t)
	called := make(map[string]bool)
	var planners []ecosystem.Runtime
	for _, name := range []string{"first", "second"} {
		planners = append(planners, invalidatingCleanupPlanner{
			name: name,
			invalidate: func(_ context.Context, group ecosystem.CacheCleanupGroup) error {
				if group.Ecosystem != name || group.Package != "orphan" || len(group.Digests) != 1 {
					t.Fatalf("unexpected orphan group: %+v", group)
				}
				called[name] = true
				return nil
			},
		})
	}
	runtime.runtimes = collectionlist.NewList[ecosystem.Runtime](planners...)
	if err := runtime.invalidatePackageCache(t.Context(), ecosystem.CacheCleanupGroup{Package: "orphan", Digests: []string{"sha256:orphan"}}); err != nil {
		t.Fatal(err)
	}
	if len(called) != 2 {
		t.Fatalf("invalidation recipients = %v", called)
	}
}

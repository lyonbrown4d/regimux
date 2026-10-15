package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/go-co-op/gocron/v2"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestDisabledManualCleanupUsesSharedDistributedLockAcrossRuntimes(t *testing.T) {
	metadata, err := meta.OpenSQLite(filepath.Join(t.TempDir(), "cache.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	baseObjects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	objects := &blockingDeleteStore{
		Store:   baseObjects,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}

	locker := &exclusiveCleanupLocker{attempts: make(chan string, 4)}
	cfg := config.DefaultConfig()
	cfg.Scheduler.Enabled = false
	cfg.Scheduler.DistributedLock = true
	cfg.Scheduler.Cleanup.Distributed = true
	cfg.Cache.Backend = "redis"
	runtimes := []*Runtime{
		newRuntimeForLockedCleanup(cfg, metadata, objects, locker),
		newRuntimeForLockedCleanup(cfg, metadata, objects, locker),
	}
	for i, runtime := range runtimes {
		if err := runtime.Start(t.Context()); err != nil {
			t.Fatalf("start runtime %d: %v", i, err)
		}
		i := i
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if stopErr := runtimes[i].Stop(ctx); stopErr != nil {
				t.Error(stopErr)
			}
		})
	}
	t.Cleanup(objects.unblock)
	digests := seedSharedCleanupArtifacts(t, metadata, baseObjects)
	for _, runtime := range runtimes {
		future := time.Now().UTC().Add(30 * 24 * time.Hour)
		runtime.cleanupNow = func() time.Time { return future }
	}

	if err := runtimes[0].TriggerCleanup(t.Context()); err != nil {
		t.Fatalf("trigger first runtime cleanup: %v", err)
	}
	select {
	case <-objects.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first cleanup did not reach object deletion")
	}
	if err := runtimes[1].TriggerCleanup(t.Context()); err != nil {
		t.Fatalf("trigger second runtime cleanup: %v", err)
	}
	firstKey := awaitCleanupLockAttempt(t, locker)
	secondKey := awaitCleanupLockAttempt(t, locker)
	if firstKey != sharedCleanupJobName || secondKey != sharedCleanupJobName {
		t.Fatalf("manual cleanup lock keys = %q, %q; want both %q", firstKey, secondKey, sharedCleanupJobName)
	}
	objects.unblock()
	waitForSharedCleanup(t, baseObjects, digests)

	if maxActive, acquired, rejected := locker.results(); maxActive != 1 || acquired != 1 || rejected != 1 {
		t.Fatalf("distributed lock max active=%d acquired=%d rejected=%d, want 1, 1, 1", maxActive, acquired, rejected)
	}
}

func newRuntimeForLockedCleanup(cfg config.Config, metadata meta.Store, objects object.Store, locker *exclusiveCleanupLocker) *Runtime {
	runtime := NewRuntime(RuntimeDependencies{
		Config: cfg, Metadata: metadata, Objects: objects,
		Runtimes: collectionlist.NewList[ecosystem.Runtime](testPackageCleanupPlanner{}),
	})
	runtime.distributedLocker = locker
	return runtime
}

func awaitCleanupLockAttempt(t *testing.T, locker *exclusiveCleanupLocker) string {
	t.Helper()
	select {
	case key := <-locker.attempts:
		return key
	case <-time.After(5 * time.Second):
		t.Fatal("manual cleanup did not attempt the distributed lock")
		return ""
	}
}

type exclusiveCleanupLocker struct {
	mu        sync.Mutex
	active    int
	maxActive int
	acquired  int
	rejected  int
	attempts  chan string
}

func (l *exclusiveCleanupLocker) Lock(_ context.Context, key string) (gocron.Lock, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active != 0 {
		l.rejected++
		l.attempts <- key
		return nil, errors.New("cleanup lock is already held")
	}
	l.active++
	l.acquired++
	if l.active > l.maxActive {
		l.maxActive = l.active
	}
	l.attempts <- key
	return exclusiveCleanupLock{locker: l}, nil
}

func (l *exclusiveCleanupLocker) results() (int, int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.maxActive, l.acquired, l.rejected
}

type exclusiveCleanupLock struct {
	locker *exclusiveCleanupLocker
}

func (l exclusiveCleanupLock) Unlock(context.Context) error {
	l.locker.mu.Lock()
	defer l.locker.mu.Unlock()
	l.locker.active--
	return nil
}

type blockingDeleteStore struct {
	object.Store
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
	releaseOnce sync.Once
}

func (s *blockingDeleteStore) unblock() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func (s *blockingDeleteStore) Delete(ctx context.Context, digest string) error {
	blocked := false
	s.once.Do(func() {
		blocked = true
		close(s.entered)
	})
	if blocked {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Store.Delete(ctx, digest)
}

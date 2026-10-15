package scheduler

import (
	"context"
	"errors"
	"fmt"
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
	fixture := newLockedCleanupFixture(t)
	if err := fixture.runtimes[0].TriggerCleanup(t.Context()); err != nil {
		t.Fatalf("trigger first runtime cleanup: %v", err)
	}
	select {
	case <-fixture.objects.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first cleanup did not reach object deletion")
	}
	if err := fixture.runtimes[1].TriggerCleanup(t.Context()); err != nil {
		t.Fatalf("trigger second runtime cleanup: %v", err)
	}
	assertSharedCleanupLockKeys(t, fixture.locker)
	fixture.objects.unblock()
	waitForSharedCleanup(t, fixture.baseObjects, fixture.digests)
	assertExclusiveCleanupLock(t, fixture.locker)
}

type lockedCleanupFixture struct {
	baseObjects object.Store
	objects     *blockingDeleteStore
	locker      *exclusiveCleanupLocker
	runtimes    []*Runtime
	digests     []string
}

func newLockedCleanupFixture(t *testing.T) lockedCleanupFixture {
	t.Helper()
	baseRuntime, metadata, baseObjects := newSharedCleanupRuntime(t)
	objects := &blockingDeleteStore{Store: baseObjects, entered: make(chan struct{}), release: make(chan struct{})}
	locker := &exclusiveCleanupLocker{attempts: make(chan string, 4)}
	cfg := baseRuntime.cfg
	cfg.Scheduler.Enabled = false
	cfg.Scheduler.DistributedLock = true
	cfg.Scheduler.Cleanup.Distributed = true
	cfg.Cache.Backend = "redis"
	runtimes := []*Runtime{
		newRuntimeForLockedCleanup(cfg, metadata, objects, locker),
		newRuntimeForLockedCleanup(cfg, metadata, objects, locker),
	}
	future := time.Now().UTC().Add(30 * 24 * time.Hour)
	for _, runtime := range runtimes {
		runtime.cleanupNow = func() time.Time { return future }
		startLockedCleanupRuntime(t, runtime)
	}
	t.Cleanup(objects.unblock)
	return lockedCleanupFixture{baseObjects: baseObjects, objects: objects, locker: locker,
		runtimes: runtimes, digests: seedSharedCleanupArtifacts(t, metadata, baseObjects)}
}

func startLockedCleanupRuntime(t *testing.T, runtime *Runtime) {
	t.Helper()
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if stopErr := runtime.Stop(ctx); stopErr != nil {
			t.Error(stopErr)
		}
	})
}

func assertSharedCleanupLockKeys(t *testing.T, locker *exclusiveCleanupLocker) {
	t.Helper()
	firstKey := awaitCleanupLockAttempt(t, locker)
	secondKey := awaitCleanupLockAttempt(t, locker)
	if firstKey != sharedCleanupJobName || secondKey != sharedCleanupJobName {
		t.Fatalf("manual cleanup lock keys = %q, %q; want both %q", firstKey, secondKey, sharedCleanupJobName)
	}
}

func assertExclusiveCleanupLock(t *testing.T, locker *exclusiveCleanupLocker) {
	t.Helper()
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
			return fmt.Errorf("blocked test object deletion: %w", ctx.Err())
		}
	}
	if err := s.Store.Delete(ctx, digest); err != nil {
		return fmt.Errorf("delete test object: %w", err)
	}
	return nil
}

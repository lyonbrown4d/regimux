package cache_test

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type countingBlobAccessMetadata struct {
	meta.Store
	releaseBlobRead chan struct{}
	releaseOnce     sync.Once
	blobReadEntered chan struct{}
	blobReadErr     error
	blobReads       atomic.Int64
	blobUpserts     atomic.Int64
	repoBlobReads   atomic.Int64
	repoBlobUpserts atomic.Int64
}

func (m *countingBlobAccessMetadata) Blob(ctx context.Context, key meta.BlobKey) (*meta.BlobRecord, bool, error) {
	m.blobReads.Add(1)
	m.blobReadEntered <- struct{}{}
	<-m.releaseBlobRead
	if m.blobReadErr != nil {
		return nil, false, m.blobReadErr
	}
	record, found, err := m.Store.Blob(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read blob metadata: %w", err)
	}
	return record, found, nil
}

func (m *countingBlobAccessMetadata) release() {
	m.releaseOnce.Do(func() { close(m.releaseBlobRead) })
}

func (m *countingBlobAccessMetadata) UpsertBlob(ctx context.Context, record meta.BlobRecord) (*meta.BlobRecord, error) {
	m.blobUpserts.Add(1)
	updated, err := m.Store.UpsertBlob(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert blob metadata: %w", err)
	}
	return updated, nil
}

func (m *countingBlobAccessMetadata) RepoBlob(ctx context.Context, key meta.RepoBlobKey) (*meta.RepoBlobRecord, bool, error) {
	m.repoBlobReads.Add(1)
	record, found, err := m.Store.RepoBlob(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read repository blob metadata: %w", err)
	}
	return record, found, nil
}

func (m *countingBlobAccessMetadata) UpsertRepoBlob(ctx context.Context, record meta.RepoBlobRecord) (*meta.RepoBlobRecord, error) {
	m.repoBlobUpserts.Add(1)
	updated, err := m.Store.UpsertRepoBlob(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert repository blob metadata: %w", err)
	}
	return updated, nil
}

type blobStatBarrier struct {
	object.Store
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	count       atomic.Int64
}

func (s *blobStatBarrier) Stat(ctx context.Context, digest string) (*object.Info, error) {
	s.count.Add(1)
	s.entered <- struct{}{}
	<-s.release
	info, err := s.Store.Stat(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("stat blob object: %w", err)
	}
	return info, nil
}

func (s *blobStatBarrier) releaseStat() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func waitForBlobTouchCallers(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var stacks bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write goroutine profile: %v", err)
		}
		if blobTouchWaiterCount(stacks.String()) >= count {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("blob touch callers in metadata or singleflight wait = fewer than %d", count)
}

func blobTouchWaiterCount(stacks string) int {
	found := 0
	for goroutine := range strings.SplitSeq(stacks, "\n\n") {
		touching := strings.Contains(goroutine, "blobProxy.touchStoredBlobAccess(")
		waiting := strings.Contains(goroutine, "singleflightx") || strings.Contains(goroutine, "countingBlobAccessMetadata).Blob(")
		if touching && waiting {
			found++
		}
	}
	return found
}

var _ meta.Store = (*countingBlobAccessMetadata)(nil)
var _ object.Store = (*blobStatBarrier)(nil)

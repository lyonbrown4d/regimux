package artifactcache_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
)

const concurrentArtifactReaders = 512

func TestGetCoalescesMetadataReadsAndOpensIndependentBodies(t *testing.T) {
	body := []byte("complete artifact body")
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	metadata := newReadMetadata(now.Add(-time.Hour), body)
	objects := &readObjectStore{body: body}
	store := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  objects,
		Now:      func() time.Time { return now },
	})
	key := artifactcache.Key{Alias: "pypi", Repository: "packages/demo", Reference: "1.2"}

	ownerResult := make(chan readResult, 1)
	go func() {
		entry, _, err := store.Get(t.Context(), key)
		ownerResult <- readResult{entry: entry, err: err}
	}()
	select {
	case <-metadata.tagStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not enter the metadata read")
	}

	var ready sync.WaitGroup
	ready.Add(concurrentArtifactReaders - 1)
	followers := make(chan readResult, concurrentArtifactReaders-1)
	for range concurrentArtifactReaders - 1 {
		go func() {
			ctx := &readWaitContext{Context: t.Context(), ready: &ready}
			entry, _, err := store.Get(ctx, key)
			followers <- readResult{entry: entry, err: err}
		}()
	}
	ready.Wait()
	close(metadata.releaseTag)

	owner := <-ownerResult
	assertReadResult(t, owner, body)
	for range concurrentArtifactReaders - 1 {
		assertReadResult(t, <-followers, body)
	}
	metadata.assertReadCounts(t, 1)
	if got := objects.gets.Load(); got != concurrentArtifactReaders {
		t.Fatalf("object reads = %d, want %d independent readers", got, concurrentArtifactReaders)
	}
}

func TestGetDoesNotCoalesceDifferentKeys(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	metadata := newReadMetadata(now.Add(-time.Hour), []byte("artifact"))
	objects := &readObjectStore{body: []byte("artifact")}
	store := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  objects,
		Now:      func() time.Time { return now },
	})
	results := make(chan readResult, 2)
	for _, reference := range []string{"1.2", "1.3"} {
		key := artifactcache.Key{Alias: "pypi", Repository: "packages/demo", Reference: reference}
		go func() {
			entry, _, err := store.Get(t.Context(), key)
			results <- readResult{entry: entry, err: err}
		}()
	}
	for range 2 {
		select {
		case <-metadata.tagStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("distinct key did not start an independent metadata read")
		}
	}
	close(metadata.releaseTag)
	for range 2 {
		assertReadResult(t, <-results, []byte("artifact"))
	}
	metadata.assertReadCounts(t, 2)
}

func TestGetCanceledFollowerDoesNotCancelSharedMetadataRead(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	metadata := newReadMetadata(now.Add(-time.Hour), []byte("artifact"))
	objects := &readObjectStore{body: []byte("artifact")}
	store := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  objects,
		Now:      func() time.Time { return now },
	})
	key := artifactcache.Key{Alias: "pypi", Repository: "packages/demo", Reference: "1.2"}
	ownerResult := make(chan readResult, 1)
	go func() {
		entry, _, err := store.Get(t.Context(), key)
		ownerResult <- readResult{entry: entry, err: err}
	}()
	select {
	case <-metadata.tagStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not enter the metadata read")
	}

	followerCtx, cancelFollower := context.WithCancel(t.Context())
	followerResult := make(chan readResult, 1)
	var ready sync.WaitGroup
	ready.Add(1)
	go func() {
		ctx := &readWaitContext{Context: followerCtx, ready: &ready}
		//nolint:contextcheck // The wrapper embeds followerCtx and only observes Done to synchronize the waiter.
		entry, _, err := store.Get(ctx, key)
		followerResult <- readResult{entry: entry, err: err}
	}()
	ready.Wait()
	cancelFollower()
	select {
	case got := <-followerResult:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled follower error = %v, want context canceled", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled follower waited for the shared metadata read")
	}
	close(metadata.releaseTag)
	assertReadResult(t, <-ownerResult, []byte("artifact"))
	metadata.assertReadCounts(t, 1)
}

type readResult struct {
	entry artifactcache.Entry
	err   error
}

func assertReadResult(t *testing.T, got readResult, body []byte) {
	t.Helper()
	if got.err != nil {
		t.Fatalf("Get artifact: %v", got.err)
	}
	if got.entry.Body == nil {
		t.Fatal("Get artifact returned a nil body")
	}
	content, err := io.ReadAll(got.entry.Body)
	if err != nil {
		t.Fatalf("read artifact body: %v", err)
	}
	if err := got.entry.Body.Close(); err != nil {
		t.Fatalf("close artifact body: %v", err)
	}
	if !bytes.Equal(content, body) {
		t.Fatalf("artifact body = %q, want %q", content, body)
	}
	if got := got.entry.Headers.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("artifact Content-Type = %q, want application/octet-stream", got)
	}
	if values := got.entry.Headers.Values("X-Test"); len(values) != 2 || values[0] != "first" || values[1] != "second" {
		t.Fatalf("artifact X-Test values = %#v, want [first second]", values)
	}
	got.entry.Headers["X-Test"][0] = "caller mutation"
}

type readMetadata struct {
	artifactcache.MetadataStore
	tag           meta.TagRecord
	manifest      meta.ManifestRecord
	tagStarted    chan struct{}
	releaseTag    chan struct{}
	tagCalls      atomic.Int64
	manifestCalls atomic.Int64
	blobCalls     atomic.Int64
	touchCalls    atomic.Int64
	lastAccess    time.Time
}

func newReadMetadata(lastAccess time.Time, body []byte) *readMetadata {
	digest := ocidigest.FromBytes(body).String()
	return &readMetadata{
		tag: meta.TagRecord{Digest: digest, ExpiresAt: time.Time{}},
		manifest: meta.ManifestRecord{
			Digest: digest,
			Size:   int64(len(body)),
			Headers: map[string][]string{
				"Content-Type": {"application/octet-stream"},
				"X-Test":       {"first", "second"},
			},
		},
		tagStarted: make(chan struct{}, concurrentArtifactReaders*2),
		releaseTag: make(chan struct{}),
		lastAccess: lastAccess,
	}
}

func (m *readMetadata) Tag(ctx context.Context, _ meta.TagKey) (*meta.TagRecord, bool, error) {
	m.tagCalls.Add(1)
	m.tagStarted <- struct{}{}
	select {
	case <-m.releaseTag:
		return &m.tag, true, nil
	case <-ctx.Done():
		return nil, false, fmt.Errorf("await metadata tag read: %w", ctx.Err())
	}
}

func (m *readMetadata) Manifest(context.Context, meta.ManifestKey) (*meta.ManifestRecord, bool, error) {
	m.manifestCalls.Add(1)
	return &m.manifest, true, nil
}

func (m *readMetadata) Blob(context.Context, meta.BlobKey) (*meta.BlobRecord, bool, error) {
	m.blobCalls.Add(1)
	return &meta.BlobRecord{Digest: m.manifest.Digest, LastAccessAt: m.lastAccess}, true, nil
}

func (m *readMetadata) TouchBlob(context.Context, meta.BlobKey, time.Time) error {
	m.touchCalls.Add(1)
	return nil
}

func (m *readMetadata) assertReadCounts(t *testing.T, want int64) {
	t.Helper()
	if got := m.tagCalls.Load(); got != want {
		t.Errorf("tag reads = %d, want %d", got, want)
	}
	if got := m.manifestCalls.Load(); got != want {
		t.Errorf("manifest reads = %d, want %d", got, want)
	}
	if got := m.blobCalls.Load(); got != want {
		t.Errorf("blob access reads = %d, want %d", got, want)
	}
	if got := m.touchCalls.Load(); got != want {
		t.Errorf("blob touches = %d, want %d", got, want)
	}
}

type readObjectStore struct {
	object.Store
	body []byte
	gets atomic.Int64
}

func (s *readObjectStore) Get(context.Context, string, object.GetOptions) (io.ReadCloser, *object.Info, error) {
	s.gets.Add(1)
	return io.NopCloser(bytes.NewReader(s.body)), &object.Info{
		Digest: ocidigest.FromBytes(s.body).String(), Size: int64(len(s.body)),
	}, nil
}

type readWaitContext struct {
	context.Context
	ready *sync.WaitGroup
	once  sync.Once
}

func (c *readWaitContext) Done() <-chan struct{} {
	c.once.Do(c.ready.Done)
	return c.Context.Done()
}

var _ artifactcache.MetadataStore = (*readMetadata)(nil)
var _ meta.BlobAccessRepository = (*readMetadata)(nil)
var _ object.Store = (*readObjectStore)(nil)

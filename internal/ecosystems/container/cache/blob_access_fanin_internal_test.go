package cache_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestBlobAccessMetadataIsFannedInWithoutSharingReaders(t *testing.T) {
	const requests = 128
	body := []byte("0123456789abcdef")
	digest := testDigestFor(body)
	fixture := newBlobAccessFanInFixture(t, digest, body, requests)
	seen := runBlobAccessFanInCohort(t, fixture, digest, requests)
	for _, got := range seen {
		assertBlobFanInResult(t, got, body)
	}
	assertBlobAccessFanInCounts(t, fixture.metadata, requests)
	assertBlobAccessFanInTimes(t, t.Context(), fixture.base, digest, fixture.old)
}

type blobAccessFanInFixture struct {
	base     meta.Store
	metadata *countingBlobAccessMetadata
	objects  *blobStatBarrier
	proxy    *cache.Proxy
	old      time.Time
}

func newBlobAccessFanInFixture(t *testing.T, digest string, body []byte, requests int) blobAccessFanInFixture {
	t.Helper()
	ctx := t.Context()
	metadata, objects := newTestStores(t)
	if _, err := objects.Put(ctx, digest, bytes.NewReader(body), object.PutOptions{ContentType: distribution.MediaTypeOctetStream}); err != nil {
		t.Fatalf("seed blob object: %v", err)
	}
	old := time.Now().UTC().Add(-time.Hour)
	if _, err := metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest: digest, Size: int64(len(body)), MediaType: distribution.MediaTypeOctetStream,
		ObjectKey: digest, LastAccessAt: old,
	}); err != nil {
		t.Fatalf("seed blob metadata: %v", err)
	}
	if _, err := metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias: "hub", Repository: "library/alpine", Digest: digest,
		LastAccessAt: old, LastVerifiedAt: old,
	}); err != nil {
		t.Fatalf("seed repository blob metadata: %v", err)
	}

	counted := &countingBlobAccessMetadata{
		Store: metadata, releaseBlobRead: make(chan struct{}), blobReadEntered: make(chan struct{}, requests),
	}
	barrierObjects := &blobStatBarrier{Store: objects, entered: make(chan struct{}, requests), release: make(chan struct{})}
	t.Cleanup(counted.release)
	t.Cleanup(barrierObjects.releaseStat)
	proxy := newTestProxy(&fakeRegistryClient{}, counted, barrierObjects, backend.NewMemory(backend.MemoryOptions{}), config.Config{})
	return blobAccessFanInFixture{base: metadata, metadata: counted, objects: barrierObjects, proxy: proxy, old: old}
}

func runBlobAccessFanInCohort(t *testing.T, fixture blobAccessFanInFixture, digest string, requests int) []blobAccessFanInResult {
	t.Helper()
	ctx := t.Context()
	results := make(chan blobAccessFanInResult, requests)
	var ready sync.WaitGroup
	ready.Add(requests)
	for i := range requests {
		req := blobAccessFanInRequest(digest, i)
		go func() {
			ready.Done()
			result, err := fixture.proxy.Blobs().Get(ctx, req)
			results <- blobAccessFanInResult{request: req, result: result, err: err}
		}()
	}
	ready.Wait()
	waitForBlobStatBarrier(t, fixture.objects.entered, requests)
	fixture.objects.releaseStat()
	waitForBlobTouchCallers(t, requests)
	fixture.metadata.release()

	seen := make([]blobAccessFanInResult, 0, requests)
	for range requests {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatalf("%s blob get: %v", got.request.Method, got.err)
			}
			if got.result == nil {
				t.Fatalf("%s blob get returned no result", got.request.Method)
			}
			seen = append(seen, got)
		case <-time.After(10 * time.Second):
			t.Fatalf("blob results incomplete: got %d of %d", len(seen), requests)
		}
	}

	return seen
}

func assertBlobAccessFanInCounts(t *testing.T, counted *countingBlobAccessMetadata, requests int) {
	t.Helper()
	if got := counted.blobReads.Load(); got != 1 {
		t.Errorf("shared blob metadata reads = %d, want 1 for the access-touch cohort", got)
	}
	if got := counted.blobUpserts.Load(); got != 1 {
		t.Errorf("shared blob metadata upserts = %d, want 1 for the access-touch cohort", got)
	}
	if got := counted.repoBlobReads.Load(); got != int64(requests+1) {
		t.Errorf("repository blob metadata reads = %d, want %d (one lookup per request plus one access touch)", got, requests+1)
	}
	if got := counted.repoBlobUpserts.Load(); got != 1 {
		t.Errorf("repository blob metadata upserts = %d, want 1 for the access-touch cohort", got)
	}
}

func assertBlobAccessFanInTimes(t *testing.T, ctx context.Context, metadata meta.Store, digest string, old time.Time) {
	t.Helper()
	updated, ok, err := metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	if err != nil || !ok {
		t.Fatalf("read touched blob metadata: ok=%v err=%v", ok, err)
	}
	if !updated.LastAccessAt.After(old) {
		t.Errorf("blob access time = %s, want after %s", updated.LastAccessAt, old)
	}
	repoBlob, ok, err := metadata.RepoBlob(ctx, meta.RepoBlobKey{Alias: "hub", Repository: "library/alpine", Digest: digest})
	if err != nil || !ok {
		t.Fatalf("read touched repository blob metadata: ok=%v err=%v", ok, err)
	}
	if !repoBlob.LastAccessAt.After(old) {
		t.Errorf("repository blob access time = %s, want after %s", repoBlob.LastAccessAt, old)
	}
	if !repoBlob.LastVerifiedAt.Equal(old) {
		t.Errorf("repository blob verification time = %s, want unchanged %s", repoBlob.LastVerifiedAt, old)
	}
}

func blobAccessFanInRequest(digest string, index int) cache.BlobRequest {
	req := cache.BlobRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet}
	switch index % 3 {
	case 1:
		req.Method = http.MethodHead
	case 2:
		req.Range = &object.HTTPRange{Start: 2, End: 5}
	}
	return req
}

type blobAccessFanInResult struct {
	request cache.BlobRequest
	result  *cache.BlobReadResult
	err     error
}

func assertBlobFanInResult(t *testing.T, got blobAccessFanInResult, body []byte) {
	t.Helper()
	content, err := io.ReadAll(got.result.Reader)
	if closeErr := got.result.Reader.Close(); closeErr != nil {
		t.Fatalf("close %s blob result: %v", got.request.Method, closeErr)
	}
	if err != nil {
		t.Fatalf("read %s blob result: %v", got.request.Method, err)
	}
	if got.request.Method == http.MethodHead {
		if len(content) != 0 || got.result.Status != http.StatusOK || got.result.Headers.Get(distribution.HeaderContentLength) != fmt.Sprint(len(body)) {
			t.Errorf("HEAD response = status %d, length %q, body %q", got.result.Status, got.result.Headers.Get(distribution.HeaderContentLength), content)
		}
		return
	}
	if got.request.Range != nil {
		if got.result.Status != http.StatusPartialContent || string(content) != string(body[2:6]) || got.result.Headers.Get(distribution.HeaderContentRange) != "bytes 2-5/16" {
			t.Errorf("range response = status %d, content-range %q, body %q", got.result.Status, got.result.Headers.Get(distribution.HeaderContentRange), content)
		}
		return
	}
	if got.result.Status != http.StatusOK || !bytes.Equal(content, body) {
		t.Errorf("GET response = status %d, body %q, want %q", got.result.Status, content, body)
	}
}

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
	return m.Store.Blob(ctx, key)
}

func (m *countingBlobAccessMetadata) release() {
	m.releaseOnce.Do(func() { close(m.releaseBlobRead) })
}

func (m *countingBlobAccessMetadata) UpsertBlob(ctx context.Context, record meta.BlobRecord) (*meta.BlobRecord, error) {
	m.blobUpserts.Add(1)
	return m.Store.UpsertBlob(ctx, record)
}

func (m *countingBlobAccessMetadata) RepoBlob(ctx context.Context, key meta.RepoBlobKey) (*meta.RepoBlobRecord, bool, error) {
	m.repoBlobReads.Add(1)
	return m.Store.RepoBlob(ctx, key)
}

func (m *countingBlobAccessMetadata) UpsertRepoBlob(ctx context.Context, record meta.RepoBlobRecord) (*meta.RepoBlobRecord, error) {
	m.repoBlobUpserts.Add(1)
	return m.Store.UpsertRepoBlob(ctx, record)
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
	return s.Store.Stat(ctx, digest)
}

func (s *blobStatBarrier) releaseStat() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func waitForBlobStatBarrier(t *testing.T, entered <-chan struct{}, count int) {
	t.Helper()
	for range count {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("not all blob requests reached the object stat barrier")
		}
	}
}

func waitForBlobTouchCallers(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var stacks bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write goroutine profile: %v", err)
		}
		found := 0
		for goroutine := range strings.SplitSeq(stacks.String(), "\n\n") {
			touching := strings.Contains(goroutine, "blobProxy.touchStoredBlobAccess(")
			waiting := strings.Contains(goroutine, "singleflightx") || strings.Contains(goroutine, "countingBlobAccessMetadata).Blob(")
			if touching && waiting {
				found++
			}
		}
		if found >= count {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("blob touch callers in metadata or singleflight wait = fewer than %d", count)
}

var _ meta.Store = (*countingBlobAccessMetadata)(nil)
var _ object.Store = (*blobStatBarrier)(nil)

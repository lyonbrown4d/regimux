package cache_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestStreamFillWaiterFallsBackAfterCachedObjectOpenFailure(t *testing.T) {
	body := []byte("streamed blob for a waiting request")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	streamBody := newGatedStreamBody(body)
	t.Cleanup(streamBody.Release)
	objectStore := &streamWaiterObjectStore{Store: objects, waiterLookup: make(chan struct{})}
	client := &streamWaiterRegistryClient{
		body:       body,
		digest:     digest,
		streamBody: streamBody,
		firstOpen:  make(chan struct{}),
	}
	proxy := newTestProxy(client, metadata, objectStore, nil, streamAndCacheConfig())
	request := cache.BlobRequest{
		UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet,
	}
	owner, err := proxy.Blobs().Get(t.Context(), request)
	if err != nil {
		t.Fatalf("start stream owner: %v", err)
	}
	if owner.Cache != cache.CacheMiss {
		t.Fatalf("owner cache status=%s, want miss", owner.Cache)
	}
	select {
	case <-client.firstOpen:
	case <-time.After(5 * time.Second):
		t.Fatal("stream owner did not fetch upstream")
	}

	baseWaiterContext, cancelWaiter := context.WithCancel(t.Context())
	defer cancelWaiter()
	waiterContext := &streamFillWaitContext{
		Context: baseWaiterContext,
		armed:   &objectStore.waiting,
		inWait:  make(chan struct{}),
	}
	waiterDone := make(chan blobGetResult, 1)
	go func() {
		result, getErr := proxy.Blobs().Get(waiterContext, request)
		waiterDone <- blobGetResult{result: result, err: getErr}
	}()
	awaitStreamFillWaiter(t, objectStore.waiterLookup, waiterContext.inWait)

	streamBody.Release()
	if got := readAndClose(t, owner.Reader); !bytes.Equal(got, body) {
		t.Fatalf("owner body=%q, want %q", got, body)
	}
	waiter := receiveBlobGetResult(t, waiterDone)
	assertStreamWaiterFallback(t, waiter, body, digest, objectStore, client)
}

func assertStreamWaiterFallback(
	t *testing.T,
	waiter blobGetResult,
	body []byte,
	digest string,
	objectStore *streamWaiterObjectStore,
	client *streamWaiterRegistryClient,
) {
	t.Helper()
	if waiter.err != nil {
		t.Fatalf("stream fill waiter after cache open failure: %v", waiter.err)
	}
	if waiter.result.Digest != digest || waiter.result.Size != int64(len(body)) {
		t.Fatalf("fallback digest/size=%q/%d, want %q/%d", waiter.result.Digest, waiter.result.Size, digest, len(body))
	}
	if waiter.result.Cache != cache.CacheBypass || waiter.result.Status != http.StatusOK {
		t.Fatalf("fallback cache/status=%s/%d, want bypass/200", waiter.result.Cache, waiter.result.Status)
	}
	waiterBody := readAndClose(t, waiter.result.Reader)
	if !bytes.Equal(waiterBody, body) || testDigestFor(waiterBody) != digest {
		t.Fatalf("waiter body/digest=%q/%q, want %q/%q", waiterBody, testDigestFor(waiterBody), body, digest)
	}
	if got := objectStore.gets.Load(); got != 1 {
		t.Fatalf("cached object Get calls=%d, want one failed waiter open", got)
	}
	if got := client.calls.Load(); got != 2 {
		t.Fatalf("upstream GET calls=%d, want stream owner plus waiter fallback", got)
	}
}

func awaitStreamFillWaiter(t *testing.T, lookup, wait <-chan struct{}) {
	t.Helper()
	select {
	case <-lookup:
	case <-time.After(5 * time.Second):
		t.Fatal("stream fill waiter did not reach its initial object lookup")
	}
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
		t.Fatal("stream fill waiter did not enter fill wait")
	}
}

func receiveBlobGetResult(t *testing.T, result <-chan blobGetResult) blobGetResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("stream fill waiter did not return")
		return blobGetResult{}
	}
}

type streamFillWaitContext struct {
	context.Context
	armed  *atomic.Bool
	inWait chan struct{}
	once   sync.Once
}

func (c *streamFillWaitContext) Done() <-chan struct{} {
	if c.armed.Load() {
		c.once.Do(func() { close(c.inWait) })
	}
	return c.Context.Done()
}

type streamWaiterObjectStore struct {
	object.Store
	waiterLookup chan struct{}
	exists       atomic.Int32
	waiting      atomic.Bool
	gets         atomic.Int32
}

func (s *streamWaiterObjectStore) Exists(ctx context.Context, digest string) (bool, error) {
	if call := s.exists.Add(1); call <= 3 {
		if call == 3 {
			s.waiting.Store(true)
			close(s.waiterLookup)
		}
		return false, nil
	}
	exists, err := s.Store.Exists(ctx, digest)
	if err != nil {
		return false, fmt.Errorf("check streamed object after fill: %w", err)
	}
	return exists, nil
}

func (s *streamWaiterObjectStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	if s.gets.Add(1) == 1 {
		return nil, nil, errors.New("injected post-fill object open failure")
	}
	reader, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("get streamed object after injected failure: %w", err)
	}
	return reader, info, nil
}

type streamWaiterRegistryClient struct {
	*fakeRegistryClient
	body       []byte
	digest     string
	streamBody *gatedStreamBody
	firstOpen  chan struct{}
	calls      atomic.Int32
}

func (c *streamWaiterRegistryClient) GetBlob(ctx context.Context, _ upstream.GetBlobRequest) (*upstream.BlobResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("get test registry blob: %w", err)
	}
	call := c.calls.Add(1)
	body := io.NopCloser(bytes.NewReader(c.body))
	if call == 1 {
		close(c.firstOpen)
		body = c.streamBody
	}
	return &upstream.BlobResponse{
		Body: body, Digest: c.digest, Size: int64(len(c.body)), StatusCode: http.StatusOK,
		Headers: http.Header{distribution.HeaderContentLength: []string{strconv.Itoa(len(c.body))}},
	}, nil
}

type gatedStreamBody struct {
	*bytes.Reader
	release     chan struct{}
	releaseOnce sync.Once
}

func newGatedStreamBody(body []byte) *gatedStreamBody {
	return &gatedStreamBody{Reader: bytes.NewReader(body), release: make(chan struct{})}
}

func (r *gatedStreamBody) Read(buffer []byte) (int, error) {
	<-r.release
	n, err := r.Reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read gated stream body: %w", err)
	}
	return n, nil
}

func (*gatedStreamBody) Close() error { return nil }

func (r *gatedStreamBody) Release() {
	r.releaseOnce.Do(func() { close(r.release) })
}

var _ upstream.RegistryClient = (*streamWaiterRegistryClient)(nil)

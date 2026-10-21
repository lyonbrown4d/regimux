package golang

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	ocidigest "github.com/opencontainers/go-digest"
)

const goWarmReaders = 128

type goWarmFixture struct {
	service  *Service
	metadata *countGoCacheMetadata
	request  Request
	body     []byte
	upstream *httptest.Server
	requests atomic.Int64
}

func newGoWarmFixture(t *testing.T) *goWarmFixture {
	t.Helper()
	metadata, objects := newGoReadStores(t)
	fixture := &goWarmFixture{
		metadata: metadata,
		request:  Request{Alias: "default", Tail: "github.com/acme/lib/@v/v1.2.3.mod"},
		body:     []byte("module github.com/acme/lib\n"),
	}
	fixture.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fixture.requests.Add(1)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("ETag", `"go-warm-cache"`)
		if _, err := w.Write(fixture.body); err != nil {
			t.Errorf("write warm Go body: %v", err)
		}
	}))
	t.Cleanup(fixture.upstream.Close)
	fixture.service = NewService(ServiceDependencies{
		Config: config.Config{Go: map[string]config.DependencyUpstreamConfig{
			"default": {Registry: fixture.upstream.URL},
		}},
		Metadata: metadata,
		Objects:  objects,
	})
	return fixture
}

func warmGoCache(t *testing.T, fixture *goWarmFixture) {
	t.Helper()
	response, err := fixture.service.Get(t.Context(), fixture.request)
	requireGoCacheNoError(t, "warm Go cache", err)
	if response.Cache != cacheMiss {
		t.Fatalf("initial Go cache status = %q, want %q", response.Cache, cacheMiss)
	}
	assertGoResponseBody(t, response, fixture.body)
}

func makeGoCacheTouchDue(t *testing.T, fixture *goWarmFixture) {
	t.Helper()
	requestRoute, err := parseRoute(fixture.request.Alias, fixture.request.Tail)
	requireGoCacheNoError(t, "parse warm Go route", err)
	tag, found, err := fixture.metadata.Store.Tag(t.Context(), meta.TagKey{
		Alias: requestRoute.Alias, Repository: requestRoute.Module, Reference: requestRoute.Reference,
	})
	requireGoCacheNoError(t, "read warm Go tag", err)
	if !found {
		t.Fatal("warm Go tag was not stored")
	}
	blob, found, err := fixture.metadata.Store.Blob(t.Context(), meta.BlobKey{Digest: tag.Digest})
	requireGoCacheNoError(t, "read warm Go blob", err)
	if !found {
		t.Fatal("warm Go blob was not stored")
	}
	blob.LastAccessAt = time.Time{}
	if _, err := fixture.metadata.UpsertBlob(t.Context(), *blob); err != nil {
		t.Fatalf("make Go cache touch due: %v", err)
	}
}

func assertGoWarmResponses(t *testing.T, responses []*Response, body []byte) {
	t.Helper()
	for index, response := range responses {
		if response.Status != http.StatusOK || response.Cache != cacheHit {
			t.Fatalf("warm response %d status/cache = %d/%q, want %d/%q", index, response.Status, response.Cache, http.StatusOK, cacheHit)
		}
		if got := response.Headers.Get("ETag"); got != `"go-warm-cache"` {
			t.Fatalf("warm response %d ETag = %q", index, got)
		}
	}
	responses[0].Headers.Set("ETag", "caller mutation")
	if got := responses[1].Headers.Get("ETag"); got != `"go-warm-cache"` {
		t.Fatalf("mutating one response header changed another caller's ETag to %q", got)
	}
	for index, response := range responses {
		assertIndependentGoBody(t, index, response.Body, body)
	}
}

func assertIndependentGoBody(t *testing.T, index int, reader io.ReadCloser, want []byte) {
	t.Helper()
	var first [1]byte
	if _, err := io.ReadFull(reader, first[:]); err != nil || first[0] != want[0] {
		t.Fatalf("warm response %d first body byte = %q, %v", index, first, err)
	}
	rest, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(rest, want[1:]) {
		t.Fatalf("warm response %d body remainder = %q, read error = %v, close error = %v", index, rest, readErr, closeErr)
	}
}

func assertGoResponseBody(t *testing.T, response *Response, want []byte) {
	t.Helper()
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(body, want) {
		t.Fatalf("Go response body = %q, read error = %v, close error = %v", body, readErr, closeErr)
	}
}

func waitForGoSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("Go cache metadata read did not reach the gate")
	}
}

func waitForGoCacheReaders(t *testing.T, metadata *countGoCacheMetadata, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	lastReaders := 0
	for time.Now().Before(deadline) {
		var stacks bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write Go cache reader profile: %v", err)
		}
		lastReaders = countGoCacheWaiters(stacks.String())
		if lastReaders >= want-1 || metadata.counts().tags >= int64(want) {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("only %d callers joined the artifact-cache metadata read; want %d", lastReaders, want-1)
}

func countGoCacheWaiters(stacks string) int {
	readers := 0
	for goroutine := range strings.SplitSeq(stacks, "\n\n") {
		if strings.Contains(goroutine, "go-singleflightx.(*Group") && strings.Contains(goroutine, "waitResult") {
			readers++
		}
	}
	return readers
}

func assertGoWarmCounts(t *testing.T, counts goCacheReadCounts, want int64) {
	t.Helper()
	if counts.tags != want || counts.manifests != want || counts.blobs != want || counts.touches != want {
		t.Fatalf("Go warm cache metadata calls tag/manifest/blob/touch = %d/%d/%d/%d, want %d each", counts.tags, counts.manifests, counts.blobs, counts.touches, want)
	}
}

type countGoCacheMetadata struct {
	meta.Store
	access        meta.BlobAccessRepository
	tagCalls      atomic.Int64
	manifestCalls atomic.Int64
	blobCalls     atomic.Int64
	touchCalls    atomic.Int64
	gate          *goTagReadGate
}

type goTagReadGate struct {
	started     chan struct{}
	release     chan struct{}
	afterRead   bool
	firstOnly   bool
	waitClaimed atomic.Bool
	releaseDo   sync.Once
}

func (m *countGoCacheMetadata) Tag(ctx context.Context, key meta.TagKey) (*meta.TagRecord, bool, error) {
	m.tagCalls.Add(1)
	if m.gate != nil && !m.gate.afterRead {
		if err := m.waitAtTagGate(ctx); err != nil {
			return nil, false, err
		}
	}
	tag, found, err := m.Store.Tag(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read Go cache tag: %w", err)
	}
	if m.gate != nil && m.gate.afterRead {
		if err := m.waitAtTagGate(ctx); err != nil {
			return nil, false, err
		}
	}
	return tag, found, nil
}

func (m *countGoCacheMetadata) waitAtTagGate(ctx context.Context) error {
	gate := m.gate
	if gate.firstOnly && !gate.waitClaimed.CompareAndSwap(false, true) {
		return nil
	}
	select {
	case gate.started <- struct{}{}:
	default:
	}
	select {
	case <-gate.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for Go cache tag gate: %w", ctx.Err())
	}
}

func (m *countGoCacheMetadata) Manifest(ctx context.Context, key meta.ManifestKey) (*meta.ManifestRecord, bool, error) {
	m.manifestCalls.Add(1)
	manifest, found, err := m.Store.Manifest(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read Go cache manifest: %w", err)
	}
	return manifest, found, nil
}

func (m *countGoCacheMetadata) Blob(ctx context.Context, key meta.BlobKey) (*meta.BlobRecord, bool, error) {
	m.blobCalls.Add(1)
	blob, found, err := m.Store.Blob(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read Go cache access record: %w", err)
	}
	return blob, found, nil
}

func (m *countGoCacheMetadata) TouchBlob(ctx context.Context, key meta.BlobKey, at time.Time) error {
	m.touchCalls.Add(1)
	if err := m.access.TouchBlob(ctx, key, at); err != nil {
		return fmt.Errorf("touch Go cache access: %w", err)
	}
	return nil
}

func (m *countGoCacheMetadata) resetCounts() {
	m.tagCalls.Store(0)
	m.manifestCalls.Store(0)
	m.blobCalls.Store(0)
	m.touchCalls.Store(0)
}

func (m *countGoCacheMetadata) counts() goCacheReadCounts {
	return goCacheReadCounts{
		tags:      m.tagCalls.Load(),
		manifests: m.manifestCalls.Load(),
		blobs:     m.blobCalls.Load(),
		touches:   m.touchCalls.Load(),
	}
}

func (m *countGoCacheMetadata) armTagGate(afterRead, firstOnly bool) (<-chan struct{}, func()) {
	gate := &goTagReadGate{
		started:   make(chan struct{}, goWarmReaders),
		release:   make(chan struct{}),
		afterRead: afterRead,
		firstOnly: firstOnly,
	}
	m.gate = gate
	return gate.started, func() { gate.releaseDo.Do(func() { close(gate.release) }) }
}

type goCacheReadCounts struct {
	tags      int64
	manifests int64
	blobs     int64
	touches   int64
}

func goBodyDigest(body []byte) string { return ocidigest.SHA256.FromBytes(body).String() }

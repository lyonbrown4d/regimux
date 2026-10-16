package pypi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestPyPIWarmWheelReadsCoalesceMetadataAndTouch(t *testing.T) {
	const clients = 128
	const wheel = "a complete cached wheel body"
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(wheel)))
		writeResponse(t, w, wheel)
	}))
	t.Cleanup(upstream.Close)

	service, metadata := newCountingMetadataService(t, upstream.URL)
	req := pypi.Request{
		Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/warm.whl"), SkipPullRecord: true,
	}
	assertInitialWarmWheel(t, service, req, wheel)
	metadata.resetCounts()
	started, release := metadata.armTagGate()
	defer release()

	results := startWarmWheelReaders(t, clients, service, req)
	testkit.WaitForSignal(t, started)
	waitForPackageManifestReaders(t, clients)
	tagsWhileBlocked := metadata.tags.Load()
	manifestsWhileBlocked := metadata.manifests.Load()
	touchesWhileBlocked := metadata.touches.Load()
	release()
	assertWarmWheelBodies(t, results.Wait(t), wheel)
	if tagsWhileBlocked != 1 || manifestsWhileBlocked != 0 || touchesWhileBlocked != 0 {
		t.Fatalf("metadata calls before release = tag:%d manifest:%d touch:%d, want 1/0/0", tagsWhileBlocked, manifestsWhileBlocked, touchesWhileBlocked)
	}
	assertMetadataCounts(t, metadata, 1, 1, 1)
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("upstream wheel requests = %d, want warm fill only", got)
	}
}

func newCountingMetadataService(t *testing.T, upstreamURL string) (*pypi.Service, *countingArtifactMetadata) {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "warm-wheel.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() { requireNoError(t, "close metadata", db.Close()) })
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open objects", err)
	counter := &countingArtifactMetadata{MetadataStore: db, access: db}
	cache := artifactcache.New(artifactcache.Dependencies{Metadata: counter, Objects: objects})
	service := pypi.NewService(pypi.ServiceDependencies{
		Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstreamURL}}},
		Cache:  cache, Metadata: db, Objects: objects,
	})
	return service, counter
}

func startWarmWheelReaders(t *testing.T, count int, service *pypi.Service, req pypi.Request) *testkit.ConcurrentRun[string] {
	t.Helper()
	return testkit.StartConcurrent(count, func() (string, error) {
		return readWarmWheel(t.Context(), service, req)
	}).WithTimeout(30 * time.Second)
}

func readWarmWheel(ctx context.Context, service *pypi.Service, req pypi.Request) (string, error) {
	response, err := service.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("get warm wheel: %w", err)
	}
	if response == nil || response.Body == nil {
		return "", errors.New("warm wheel response has no body")
	}
	if response.Cache != artifactcache.CacheHit {
		if closeErr := response.Body.Close(); closeErr != nil {
			return "", fmt.Errorf("close unexpected warm wheel response: %w", closeErr)
		}
		return "", fmt.Errorf("warm wheel cache status = %q, want hit", response.Cache)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return "", fmt.Errorf("read warm wheel body: %w", readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close warm wheel body: %w", closeErr)
	}
	return string(body), nil
}

func assertInitialWarmWheel(t *testing.T, service *pypi.Service, req pypi.Request, want string) {
	t.Helper()
	response, err := service.Get(t.Context(), req)
	requireNoError(t, "get warm wheel", err)
	if response == nil || response.Body == nil {
		t.Fatal("warm wheel response has no body")
	}
	if response.Cache != artifactcache.CacheMiss {
		t.Fatalf("initial wheel cache status = %q, want miss", response.Cache)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		t.Fatalf("read warm wheel: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close warm wheel: %v", closeErr)
	}
	if string(body) != want {
		t.Fatalf("warm wheel body = %q, want complete body", string(body))
	}
}

func assertWarmWheelBodies(t *testing.T, bodies []string, want string) {
	t.Helper()
	for index, body := range bodies {
		if body != want {
			t.Fatalf("warm wheel client %d body = %q, want complete body", index, body)
		}
	}
}

func waitForPackageManifestReaders(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	lastReaders := 0
	for time.Now().Before(deadline) {
		var stacks bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write goroutine profile: %v", err)
		}
		readers := 0
		for goroutine := range strings.SplitSeq(stacks.String(), "\n\n") {
			if strings.Contains(goroutine, "pypi.(*Service).packageManifest") {
				readers++
			}
		}
		lastReaders = readers
		if readers >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("only %d callers reached packageManifest; want %d", lastReaders, want)
}

func assertMetadataCounts(t *testing.T, metadata *countingArtifactMetadata, tags, manifests, touches int32) {
	t.Helper()
	if got := metadata.tags.Load(); got != tags {
		t.Fatalf("metadata tag lookups = %d, want %d", got, tags)
	}
	if got := metadata.manifests.Load(); got != manifests {
		t.Fatalf("metadata manifest lookups = %d, want %d", got, manifests)
	}
	if got := metadata.touches.Load(); got != touches {
		t.Fatalf("metadata access touches = %d, want %d", got, touches)
	}
}

type countingArtifactMetadata struct {
	artifactcache.MetadataStore
	access         meta.BlobAccessRepository
	tags           atomic.Int32
	manifests      atomic.Int32
	touches        atomic.Int32
	tagGate        <-chan struct{}
	tagStarted     chan struct{}
	tagStartedOnce sync.Once
	tagReleaseOnce sync.Once
}

func (m *countingArtifactMetadata) Tag(ctx context.Context, key meta.TagKey) (*meta.TagRecord, bool, error) {
	m.tags.Add(1)
	if m.tagGate != nil {
		m.tagStartedOnce.Do(func() { close(m.tagStarted) })
		select {
		case <-m.tagGate:
		case <-ctx.Done():
			return nil, false, fmt.Errorf("wait for metadata tag gate: %w", ctx.Err())
		}
	}
	tag, ok, err := m.MetadataStore.Tag(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("counted metadata tag lookup: %w", err)
	}
	return tag, ok, nil
}

func (m *countingArtifactMetadata) Manifest(ctx context.Context, key meta.ManifestKey) (*meta.ManifestRecord, bool, error) {
	m.manifests.Add(1)
	manifest, ok, err := m.MetadataStore.Manifest(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("counted metadata manifest lookup: %w", err)
	}
	return manifest, ok, nil
}

func (m *countingArtifactMetadata) TouchBlob(ctx context.Context, key meta.BlobKey, at time.Time) error {
	m.touches.Add(1)
	if err := m.access.TouchBlob(ctx, key, at); err != nil {
		return fmt.Errorf("counted metadata access touch: %w", err)
	}
	return nil
}

func (m *countingArtifactMetadata) resetCounts() {
	m.tags.Store(0)
	m.manifests.Store(0)
	m.touches.Store(0)
}

func (m *countingArtifactMetadata) armTagGate() (<-chan struct{}, func()) {
	release := make(chan struct{})
	m.tagStarted = make(chan struct{})
	m.tagGate = release
	return m.tagStarted, func() { m.tagReleaseOnce.Do(func() { close(release) }) }
}

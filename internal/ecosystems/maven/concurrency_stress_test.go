//nolint:testpackage // Stress tests exercise public Get with controlled fill-pool fixtures.
package maven

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestMaven512ConcurrentReleaseArtifactMissCoalesces(t *testing.T) {
	const clients = 512
	upstream, requests, started, release := newMavenReleaseFixture(t)
	local, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open local objects: %v", err)
	}
	service, _ := newAsyncFillService(t, upstream.URL, local, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	run, entered := startMavenConcurrentGets(service, ctx, clients)
	waitMavenStressSignal(t, started, "first upstream request")
	waitMavenEntries(t, entered, clients)
	release()

	assertMavenCoalescedResponses(t, run.Wait(t), clients)
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func newMavenReleaseFixture(t *testing.T) (*httptest.Server, *atomic.Int32, <-chan struct{}, func()) {
	t.Helper()
	var requests atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := newAsyncUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		if _, err := io.WriteString(w, slowWritePOM); err != nil {
			t.Errorf("write POM: %v", err)
		}
	})
	t.Cleanup(closeRelease)
	return upstream, &requests, started, closeRelease
}

func startMavenConcurrentGets(service *Service, ctx context.Context, clients int) (*testkit.ConcurrentRun[*Response], <-chan struct{}) {
	entered := make(chan struct{}, clients)
	run := testkit.StartConcurrent(clients, func() (*Response, error) {
		entered <- struct{}{}
		return service.Get(ctx, Request{Alias: "central", Tail: asyncFillFirstTail, SkipPullRecord: true})
	}).WithTimeout(45 * time.Second)
	return run, entered
}

func waitMavenEntries(t *testing.T, entered <-chan struct{}, count int) {
	t.Helper()
	for range count {
		<-entered
	}
}

func assertMavenCoalescedResponses(t *testing.T, responses []*Response, clients int) {
	t.Helper()
	misses, hits := mavenCacheStatusCounts(t, responses)
	if misses != 1 || hits != clients-1 {
		t.Fatalf("cache statuses: misses=%d hits=%d, want 1 miss and %d hits", misses, hits, clients-1)
	}
}

func mavenCacheStatusCounts(t *testing.T, responses []*Response) (int, int) {
	t.Helper()
	misses, hits := 0, 0
	for _, response := range responses {
		assertMavenResponseBody(t, response)
		misses, hits = addMavenCacheStatus(t, response.Cache, misses, hits)
	}
	return misses, hits
}

func assertMavenResponseBody(t *testing.T, response *Response) {
	t.Helper()
	if response == nil || response.Status != http.StatusOK || response.Body == nil {
		t.Fatalf("invalid Maven response: %#v", response)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		t.Fatalf("read Maven response body: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close Maven response body: %v", closeErr)
	}
	if string(body) != slowWritePOM {
		t.Fatalf("Maven body = %q, want %q", body, slowWritePOM)
	}
}

func addMavenCacheStatus(t *testing.T, status string, misses, hits int) (int, int) {
	t.Helper()
	switch status {
	case cacheMiss:
		misses++
	case cacheHit:
		hits++
	default:
		t.Fatalf("unexpected cache status %q", status)
	}
	return misses, hits
}

func TestMavenFillPoolCapsDistinctCacheWritesAndValidatesBypass(t *testing.T) {
	const fillWorkers = defaultMavenFillConcurrency
	const bypassRequests = 24
	writes, service, metadata, upstreamRequests, ctx, cancel, releaseWrites := newMavenFillPoolFixture(t, fillWorkers, bypassRequests)
	defer cancel()
	startMavenFillWorkers(t, service, ctx, writes, fillWorkers)
	if got := writes.puts.Load(); got != fillWorkers {
		t.Fatalf("blocked cache writes = %d, want %d", got, fillWorkers)
	}

	runMavenBypassWave(t, service, ctx, bypassRequests)
	if got := upstreamRequests.Load(); got != fillWorkers+bypassRequests {
		t.Fatalf("upstream requests = %d, want %d", got, fillWorkers+bypassRequests)
	}
	if got := writes.puts.Load(); got != fillWorkers {
		t.Fatalf("cache Put calls while pool saturated = %d, want %d", got, fillWorkers)
	}
	assertMavenTagsAbsent(t, metadata, ctx, "bypass", bypassRequests, "bypassed artifact")
	assertMavenTagsAbsent(t, metadata, ctx, "fill", fillWorkers, "blocked artifact")

	releaseWrites()
	assertMavenFillArtifacts(t, metadata, writes, fillWorkers)
}

func newMavenFillPoolFixture(t *testing.T, fillWorkers, bypassRequests int) (*stressBlockingMavenWrites, *Service, meta.Store, *atomic.Int32, context.Context, context.CancelFunc, func()) {
	t.Helper()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writes := &stressBlockingMavenWrites{
		Store:   objects,
		entered: make(chan struct{}, fillWorkers+bypassRequests),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseWrites := func() { releaseOnce.Do(func() { close(writes.release) }) }

	var upstreamRequests atomic.Int32
	upstream := newAsyncUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamRequests.Add(1)
		name := strings.TrimSuffix(strings.TrimSuffix(r.URL.Path, ".pom"), "/")
		name = name[strings.LastIndex(name, "/")+1:]
		name = strings.TrimSuffix(name, "-1.0")
		w.Header().Set("Content-Type", "application/xml")
		if _, writeErr := io.WriteString(w, stressPOM(name)); writeErr != nil {
			t.Errorf("write POM: %v", writeErr)
		}
	})
	service, metadata := newAsyncFillService(t, upstream.URL, writes, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	t.Cleanup(releaseWrites)
	return writes, service, metadata, &upstreamRequests, ctx, cancel, releaseWrites
}

func startMavenFillWorkers(t *testing.T, service *Service, ctx context.Context, writes *stressBlockingMavenWrites, fillWorkers int) {
	t.Helper()
	for i := range fillWorkers {
		name := fmt.Sprintf("fill%d", i)
		response, getErr := service.Get(ctx, Request{Alias: "central", Tail: stressMavenTail(name), SkipPullRecord: true})
		if getErr != nil {
			t.Fatalf("Get %s: %v", name, getErr)
		}
		assertStressMavenBody(t, response, stressPOM(name))
	}
	for range fillWorkers {
		waitMavenStressSignal(t, writes.entered, "cache write occupying fill worker")
	}
}

func runMavenBypassWave(t *testing.T, service *Service, ctx context.Context, bypassRequests int) {
	t.Helper()
	entered := make(chan struct{}, bypassRequests)
	var nextBypass atomic.Int32
	run := testkit.StartConcurrent(bypassRequests, func() (string, error) {
		entered <- struct{}{}
		name := fmt.Sprintf("bypass%d", nextBypass.Add(1)-1)
		response, getErr := service.Get(ctx, Request{Alias: "central", Tail: stressMavenTail(name), SkipPullRecord: true})
		if getErr != nil {
			return "", fmt.Errorf("get bypass artifact %s: %w", name, getErr)
		}
		return assertStressMavenBodyValue(response, stressPOM(name))
	}).WithTimeout(30 * time.Second)
	for range bypassRequests {
		<-entered
	}
	for _, result := range run.Wait(t) {
		if result != "validated-bypass" {
			t.Fatalf("bypass result = %q", result)
		}
	}
}

func assertMavenTagsAbsent(t *testing.T, metadata meta.Store, ctx context.Context, prefix string, count int, description string) {
	t.Helper()
	for i := range count {
		key := stressMavenTagKey(fmt.Sprintf("%s%d", prefix, i))
		if _, ok, tagErr := metadata.Tag(ctx, key); tagErr != nil || ok {
			t.Fatalf("%s tag %q: ok=%t err=%v, want absent", description, key.Reference, ok, tagErr)
		}
	}
}

func assertMavenFillArtifacts(t *testing.T, metadata meta.Store, writes *stressBlockingMavenWrites, fillWorkers int) {
	t.Helper()
	for i := range fillWorkers {
		assertAsyncMavenCache(t, metadata, writes.Store, stressMavenTagKey(fmt.Sprintf("fill%d", i)), stressPOM(fmt.Sprintf("fill%d", i)))
	}
}

type stressBlockingMavenWrites struct {
	object.Store
	entered chan struct{}
	release chan struct{}
	puts    atomic.Int32
}

func (s *stressBlockingMavenWrites) Put(ctx context.Context, key string, body io.Reader, opts object.PutOptions) (*object.Info, error) {
	s.puts.Add(1)
	select {
	case s.entered <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("await Maven cache slot: %w", ctx.Err())
	}
	select {
	case <-s.release:
		info, err := s.Store.Put(ctx, key, body, opts)
		if err != nil {
			return nil, fmt.Errorf("store Maven object: %w", err)
		}
		return info, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("await Maven write release: %w", ctx.Err())
	}
}

func stressMavenTail(name string) string {
	return "com/acme/" + name + "/1.0/" + name + "-1.0.pom"
}

func stressPOM(name string) string {
	return fmt.Sprintf("<project><modelVersion>4.0.0</modelVersion><groupId>com.acme</groupId><artifactId>%s</artifactId><version>1.0</version></project>", name)
}

func stressMavenTagKey(name string) meta.TagKey {
	return meta.TagKey{Alias: "central", Repository: "com/acme/" + name + "/1.0", Reference: name + "-1.0.pom"}
}

func assertStressMavenBody(t *testing.T, response *Response, want string) {
	t.Helper()
	got, err := assertStressMavenBodyValue(response, want)
	if err != nil || got != "validated-bypass" {
		t.Fatalf("Maven response validation = %q, err=%v", got, err)
	}
}

func assertStressMavenBodyValue(response *Response, want string) (string, error) {
	if response == nil || response.Status != http.StatusOK || response.Body == nil || response.Cache != cacheMiss {
		return "", fmt.Errorf("invalid Maven response: %#v", response)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return "", fmt.Errorf("read Maven response body: %w", readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close Maven response body: %w", closeErr)
	}
	if string(body) != want {
		return "", fmt.Errorf("Maven body=%q want=%q", body, want)
	}
	return "validated-bypass", nil
}

func waitMavenStressSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

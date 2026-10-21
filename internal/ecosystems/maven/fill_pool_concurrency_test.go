//nolint:testpackage // Stress tests exercise public Get with controlled fill-pool fixtures.
package maven

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestMavenFillPoolCapsDistinctCacheWritesAndValidatesBypass(t *testing.T) {
	const fillWorkers = defaultMavenFillConcurrency
	const bypassRequests = 24
	fixture := newMavenFillPoolFixture(t, fillWorkers, bypassRequests)
	writes, service, metadata, upstreamRequests, ctx, cancel, releaseWrites := fixture.writes, fixture.service, fixture.metadata, fixture.upstreamRequests, fixture.ctx, fixture.cancel, fixture.releaseWrites
	defer cancel()
	startMavenFillWorkers(ctx, t, service, writes, fillWorkers)
	if got := writes.puts.Load(); got != fillWorkers {
		t.Fatalf("blocked cache writes = %d, want %d", got, fillWorkers)
	}

	runMavenBypassWave(ctx, t, service, bypassRequests)
	if got := upstreamRequests.Load(); got != fillWorkers+bypassRequests {
		t.Fatalf("upstream requests = %d, want %d", got, fillWorkers+bypassRequests)
	}
	if got := writes.puts.Load(); got != fillWorkers {
		t.Fatalf("cache Put calls while pool saturated = %d, want %d", got, fillWorkers)
	}
	assertMavenTagsAbsent(ctx, t, metadata, "bypass", bypassRequests, "bypassed artifact")
	assertMavenTagsAbsent(ctx, t, metadata, "fill", fillWorkers, "blocked artifact")

	releaseWrites()
	assertMavenFillArtifacts(t, metadata, writes, fillWorkers)
}

type mavenFillPoolFixture struct {
	writes           *stressBlockingMavenWrites
	service          *Service
	metadata         meta.Store
	upstreamRequests *atomic.Int32
	ctx              context.Context
	cancel           context.CancelFunc
	releaseWrites    func()
}

func newMavenFillPoolFixture(t *testing.T, fillWorkers, bypassRequests int) mavenFillPoolFixture {
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
	return mavenFillPoolFixture{writes: writes, service: service, metadata: metadata, upstreamRequests: &upstreamRequests, ctx: ctx, cancel: cancel, releaseWrites: releaseWrites}
}

func startMavenFillWorkers(ctx context.Context, t *testing.T, service *Service, writes *stressBlockingMavenWrites, fillWorkers int) {
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

func runMavenBypassWave(ctx context.Context, t *testing.T, service *Service, bypassRequests int) {
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

func assertMavenTagsAbsent(ctx context.Context, t *testing.T, metadata meta.Store, prefix string, count int, description string) {
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

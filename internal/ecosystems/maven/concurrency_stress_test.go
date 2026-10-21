//nolint:testpackage // Stress tests exercise public Get with controlled fill-pool fixtures.
package maven

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	run, entered := startMavenConcurrentGets(ctx, service, clients)
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

func startMavenConcurrentGets(ctx context.Context, service *Service, clients int) (*testkit.ConcurrentRun[*Response], <-chan struct{}) {
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

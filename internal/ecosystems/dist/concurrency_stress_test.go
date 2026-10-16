package dist_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/testkit"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestDist512ConcurrentFullArtifactMissCoalesces(t *testing.T) {
	const clients = 512
	const body = "deterministic-dist-stress-payload"
	var requests atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set(distribution.HeaderContentType, "application/octet-stream")
		writeBody(t, w, body)
	}))
	t.Cleanup(upstream.Close)
	service, _, _ := newTestService(t.Context(), t, upstream.URL, []string{"stress.bin"})

	entered := make(chan struct{}, clients)
	run := testkit.StartConcurrent(clients, func() (*dist.Response, error) {
		entered <- struct{}{}
		return service.Get(t.Context(), dist.Request{Alias: "gradle", Tail: "stress.bin", Method: http.MethodGet, SkipPullRecord: true})
	}).WithTimeout(45 * time.Second)
	testkit.WaitForSignal(t, started)
	for range clients {
		<-entered
	}
	releaseOnce.Do(func() { close(release) })

	responses := run.RequireOneMiss(t, artifactcache.CacheMiss, artifactcache.CacheHit, func(resp *dist.Response) string {
		if got := readResponse(t, resp); got != body {
			t.Fatalf("body = %q, want %q", got, body)
		}
		return resp.Cache
	})
	if len(responses) != clients {
		t.Fatalf("responses = %d, want %d", len(responses), clients)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

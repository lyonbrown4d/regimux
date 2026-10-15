package maven_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type blockedGetsStore struct {
	object.Store
	calls atomic.Int64
}

func (s *blockedGetsStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	if s.calls.Add(1) <= 2 {
		<-ctx.Done()
		return nil, nil, fmt.Errorf("read blocking cache object: %w", ctx.Err())
	}
	body, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("get blocking cache object: %w", err)
	}
	return body, info, nil
}

func TestServiceBoundsCacheReadAndFallsBackToUpstream(t *testing.T) {
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		writeResponse(t, w, "healthy artifact")
	}))
	t.Cleanup(upstream.Close)

	ctx := context.Background()
	upstreams := map[string]config.DependencyUpstreamConfig{"central": {Registry: upstream.URL}}
	seeded, metadata, objects := newTestServiceWithStores(ctx, t, upstreams)
	seededResponse, err := seeded.Get(ctx, maven.Request{Alias: "central", Tail: "com/acme/demo/1.0/demo-1.0.jar"})
	requireNoError(t, "seed cache", err)
	assertBody(t, seededResponse, "healthy artifact")

	blocked := &blockedGetsStore{Store: objects}
	stalledCache := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: blocked})
	stalled := maven.NewService(maven.ServiceDependencies{
		Config: config.Config{Maven: upstreams}, Cache: stalledCache, Metadata: metadata, Objects: blocked,
	})
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	started := time.Now()
	response, err := stalled.Get(requestCtx, maven.Request{Alias: "central", Tail: "com/acme/demo/1.0/demo-1.0.jar"})
	elapsed := time.Since(started)
	requireNoError(t, "get after cache read stall", err)
	assertBody(t, response, "healthy artifact")
	if elapsed >= 4*time.Second {
		t.Fatalf("request took %s, want cache-read timeout to fall back before caller deadline", elapsed)
	}
	if got := upstreamCalls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want seed plus fallback", got)
	}
	if got := blocked.calls.Load(); got < 3 {
		t.Fatalf("object Get calls = %d, want two blocked cache lookups plus cache reopen", got)
	}
}

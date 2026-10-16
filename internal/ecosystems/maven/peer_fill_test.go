//nolint:testpackage // Simulate another lease owner publishing the same shared artifact.
package maven

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

type publishingPeerMavenLocker struct {
	cache *artifactcache.Store
	key   artifactcache.Key
}

func (p publishingPeerMavenLocker) AcquireLease(ctx context.Context, _ string, _ time.Duration) (artifactcache.FillLease, bool, error) {
	entry, err := p.cache.Put(ctx, artifactcache.PutRequest{
		Key: p.key, AcceptKey: acceptKeyMaven, Body: strings.NewReader(slowWritePOM),
		Headers: http.Header{"Content-Type": []string{"application/xml"}}, ContentType: "application/xml",
	})
	closeReadCloser(entry.Body, nil, "close peer cache publication")
	return nil, false, wrapError(err, "publish peer Maven fill")
}

func TestMavenFillUsesArtifactCommittedByPeerWithoutFetchingAgain(t *testing.T) {
	_, metadata := newCacheDegradationService(t)
	local, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	peer := &publishingPeerMavenLocker{
		key: artifactcache.Key{Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.pom"},
	}
	cache := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata, Objects: local,
		Fills: artifactcache.NewFillTracker(artifactcache.WithFillLocker(peer)),
	})
	peer.cache = cache
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	service := NewService(ServiceDependencies{
		Config:   config.Config{Maven: map[string]config.DependencyUpstreamConfig{"central": {Registry: upstream.URL}}},
		Metadata: metadata, Cache: cache,
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	response, err := service.Get(t.Context(), Request{Alias: "central", Tail: "com/acme/demo/1.0/demo-1.0.pom"})
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, slowWritePOM, string(body))
	require.Equal(t, cacheHit, response.Cache)
	require.Zero(t, upstreamCalls.Load())
}

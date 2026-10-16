//nolint:testpackage // The shared pending replay must bypass cache I/O for local waiters.
package maven

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

type stalledMavenReadStore struct {
	object.Store
	entered chan struct{}
	once    sync.Once
}

func (s *stalledMavenReadStore) Get(ctx context.Context, _ string, _ object.GetOptions) (io.ReadCloser, *object.Info, error) {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return nil, nil, wrapError(ctx.Err(), "read stalled Maven object")
}

func TestPendingMavenResponseBypassesStalledCacheRead(t *testing.T) {
	_, metadata := newCacheDegradationService(t)
	local, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	objects := &stalledMavenReadStore{Store: local, entered: make(chan struct{})}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, writeErr := io.WriteString(w, slowWritePOM)
		require.NoError(t, writeErr)
	}))
	t.Cleanup(upstream.Close)
	service := NewService(ServiceDependencies{
		Config:   config.Config{Maven: map[string]config.DependencyUpstreamConfig{"central": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: objects,
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	req := Request{Alias: "central", Tail: "com/acme/demo/1.0/demo-1.0.pom"}
	first, err := service.Get(t.Context(), req)
	require.NoError(t, err)
	firstBody, readErr := io.ReadAll(first.Body)
	require.NoError(t, readErr)
	require.Equal(t, slowWritePOM, string(firstBody))
	require.NoError(t, first.Body.Close())
	waitSlowCacheSignal(t, objects.entered, "post-write cache read")
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	second, err := service.Get(ctx, req)
	require.NoError(t, err, "ready local replay must bypass stalled object Get")
	secondBody, readErr := io.ReadAll(second.Body)
	require.NoError(t, readErr)
	require.Equal(t, slowWritePOM, string(secondBody))
	require.NoError(t, second.Body.Close())
	require.NoError(t, ctx.Err())
}

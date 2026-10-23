//nolint:testpackage // The shared pending replay must bypass cache I/O for local waiters.
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

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

type stalledMavenReadStore struct {
	object.Store
	gets atomic.Int32
}

func (s *stalledMavenReadStore) Get(ctx context.Context, _ string, _ object.GetOptions) (io.ReadCloser, *object.Info, error) {
	s.gets.Add(1)
	<-ctx.Done()
	return nil, nil, wrapError(ctx.Err(), "read stalled Maven object")
}

type committedTagPauseMetadata struct {
	meta.Store
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (s *committedTagPauseMetadata) UpsertTag(ctx context.Context, record meta.TagRecord) (*meta.TagRecord, error) {
	tag, err := s.Store.UpsertTag(ctx, record)
	if err != nil {
		return nil, wrapError(err, "commit pending Maven fixture tag")
	}
	s.once.Do(func() { close(s.entered) })
	select {
	case <-s.release:
		return tag, nil
	case <-ctx.Done():
		return nil, wrapError(ctx.Err(), "wait for pending Maven fixture release")
	}
}

func TestPendingMavenResponseBypassesStalledCacheRead(t *testing.T) {
	_, metadata := newCacheDegradationService(t)
	local, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	objects := &stalledMavenReadStore{Store: local}
	metadataEntered := make(chan struct{})
	releaseMetadata := make(chan struct{})
	var releaseOnce sync.Once
	var upstreamRequests atomic.Int32
	metadata = &committedTagPauseMetadata{
		Store: metadata, entered: metadataEntered, release: releaseMetadata,
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		_, writeErr := io.WriteString(w, slowWritePOM)
		require.NoError(t, writeErr)
	}))
	t.Cleanup(upstream.Close)
	service := NewService(ServiceDependencies{
		Config:   config.Config{Maven: map[string]config.DependencyUpstreamConfig{"central": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: objects,
	})
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseMetadata) }) })
	req := Request{Alias: "central", Tail: "com/acme/demo/1.0/demo-1.0.pom"}
	first, err := service.Get(t.Context(), req)
	require.NoError(t, err)
	firstBody, readErr := io.ReadAll(first.Body)
	require.NoError(t, readErr)
	require.Equal(t, slowWritePOM, string(firstBody))
	require.NoError(t, first.Body.Close())
	waitSlowCacheSignal(t, metadataEntered, "committed tag metadata write")
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	second, err := service.Get(ctx, req)
	require.NoError(t, err, "ready local replay must bypass stalled object Get")
	secondBody, readErr := io.ReadAll(second.Body)
	require.NoError(t, readErr)
	require.Equal(t, slowWritePOM, string(secondBody))
	require.NoError(t, second.Body.Close())
	require.NoError(t, ctx.Err())
	require.Zero(t, objects.gets.Load(), "pending replay must bypass object-store reads")
	require.EqualValues(t, 1, upstreamRequests.Load(), "the second request must join the first upstream fetch")
}

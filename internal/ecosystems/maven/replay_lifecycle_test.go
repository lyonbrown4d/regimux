//nolint:testpackage // Replay ownership is shared by the response and fill worker.
package maven

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

func TestMavenReplayWaitsForEveryReaderBeforeRemoval(t *testing.T) {
	for _, first := range []int{0, 1} {
		t.Run([]string{"response_first", "writer_first"}[first], func(t *testing.T) {
			replay, err := newReplayableBody(t.Context(), io.NopCloser(strings.NewReader(slowWritePOM)))
			require.NoError(t, err)
			t.Cleanup(replay.Remove)
			readers := make([]io.ReadCloser, 2)
			for index := range readers {
				readers[index], err = replay.Open(false)
				require.NoError(t, err)
				t.Cleanup(func() { closeReadCloser(readers[index], nil, "close test replay") })
			}
			replay.Remove()
			require.NoError(t, readers[first].Close())
			require.NoError(t, readers[first].Close(), "repeated close")
			require.FileExists(t, replay.path, "replay removed before final reader closed")
			body, err := io.ReadAll(readers[1-first])
			require.NoError(t, err)
			require.Equal(t, slowWritePOM, string(body))
			require.NoError(t, readers[1-first].Close())
			require.NoFileExists(t, replay.path, "replay not removed after final close")
		})
	}
}

func TestCloseMavenServiceCancelsWriteWithoutDiscardingResponse(t *testing.T) {
	_, metadata := newCacheDegradationService(t)
	local, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	objects := &blockingMavenWriteStore{Store: local, entered: make(chan struct{}), release: make(chan struct{})}
	service := NewService(ServiceDependencies{Metadata: metadata, Objects: objects})
	t.Cleanup(func() {
		close(objects.release)
		if closeErr := service.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	response, err := service.responseFromFetched(t.Context(), Request{Method: http.MethodGet}, Route{
		Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.pom",
	}, &upstreamFetch{
		status: http.StatusOK, headers: http.Header{"Content-Type": []string{"application/xml"}},
		body: io.NopCloser(strings.NewReader(slowWritePOM)),
	})
	require.NoError(t, err)
	t.Cleanup(func() { closeReadCloser(response.Body, nil, "close test response") })
	waitSlowCacheSignal(t, objects.entered, "cache write before shutdown")
	reader, ok := response.Body.(*replayableReadCloser)
	require.True(t, ok, "response must retain a replay reader")
	path := reader.body.path
	started := time.Now()
	require.NoError(t, service.Close())
	require.Less(t, time.Since(started), time.Second, "shutdown must cancel the cache write")
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, slowWritePOM, string(body))
	require.NoError(t, response.Body.Close())
	require.NoFileExists(t, path, "replay leaked after shutdown and response close")
	_, found, tagErr := metadata.Tag(t.Context(), slowCacheHTTPTagKey())
	require.NoError(t, tagErr)
	require.False(t, found, "canceled write must not publish cache metadata")
}

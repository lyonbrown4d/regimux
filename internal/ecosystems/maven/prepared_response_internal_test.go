package maven

import (
	"context"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

func TestResponseFromFetchedWithoutCacheHandlesNilHeadersAndRemovesReplay(t *testing.T) {
	service := &Service{}
	response, err := service.responseFromFetched(context.Background(), Request{Method: http.MethodGet}, testPreparedPOMRoute("nil-headers"), testFetchedPOM(io.NopCloser(strings.NewReader(slowWritePOM))))
	require.NoError(t, err)
	require.NotNil(t, response)
	require.Equal(t, int64(len(slowWritePOM)), response.Size)
	require.Equal(t, strconv.Itoa(len(slowWritePOM)), response.Headers.Get("Content-Length"))
	require.Equal(t, "application/xml", response.ContentType)
	replayBody, ok := response.Body.(*replayableReadCloser)
	require.True(t, ok)
	replayPath := replayBody.body.path
	require.Equal(t, slowWritePOM, readAndClosePreparedTestBody(t, response.Body))
	require.NoFileExists(t, replayPath)
}

func TestResponseFromFetchedHonorsCancellationDuringUpstreamClose(t *testing.T) {
	store := &countingMavenObjectStore{Store: newMavenObjectStore(t)}
	service := newPreparedResponseTestService(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	body := &cancelOnCloseMavenBody{Reader: strings.NewReader(slowWritePOM), cancel: cancel}

	response, err := service.responseFromFetched(ctx, Request{Method: http.MethodGet}, testPreparedPOMRoute("close-cancel"), testFetchedPOM(body))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, response)
	require.Equal(t, len(slowWritePOM), body.readBytes)
	require.True(t, body.closed)
	require.NoError(t, service.Close())
	require.Zero(t, store.puts.Load(), "canceled preparation must not start a cache write")
}

func TestResponseFromFetchedAfterServiceCloseRejectsFillAndRemovesReplay(t *testing.T) {
	service, _ := newCacheDegradationService(t)
	require.NoError(t, service.Close())
	before := preparedResponseReplayPaths(t)

	response, err := service.responseFromFetched(context.Background(), Request{Method: http.MethodGet}, testPreparedPOMRoute("closed-service"), testFetchedPOM(io.NopCloser(strings.NewReader(slowWritePOM))))
	require.Error(t, err)
	require.Nil(t, response)
	require.Equal(t, before, preparedResponseReplayPaths(t), "rejected fill must remove its prepared replay")
}

func TestResponseFromFetchedCapacityOneBypassesOtherRouteAndCleansJoinedReplay(t *testing.T) {
	local := newMavenObjectStore(t)
	objects := &blockingMavenWriteStore{Store: local, entered: make(chan struct{}), release: make(chan struct{})}
	service := newPreparedResponseTestService(t, objects)
	pool, err := newMavenFillPool(1)
	require.NoError(t, err)
	service.fillPool = pool
	t.Cleanup(func() {
		close(objects.release)
		require.NoError(t, service.Close())
	})

	firstRoute := testPreparedPOMRoute("pool-first")
	baselinePaths := preparedResponseReplayPaths(t)
	first, err := service.responseFromFetched(context.Background(), Request{Method: http.MethodGet}, firstRoute, testFetchedPOM(io.NopCloser(strings.NewReader(slowWritePOM))))
	require.NoError(t, err)
	require.Equal(t, slowWritePOM, readPreparedTestBody(t, first.Body))
	waitPreparedResponseSignal(t, objects.entered)
	pathsWithPendingFill := preparedResponseReplayPaths(t)
	require.Len(t, pathsWithPendingFill, len(baselinePaths)+1)

	otherRoute := testPreparedPOMRoute("pool-other")
	other, err := service.responseFromFetched(context.Background(), Request{Method: http.MethodGet}, otherRoute, testFetchedPOM(io.NopCloser(strings.NewReader(slowWritePOM))))
	require.NoError(t, err, "saturated fill pool should serve a validated response without caching")
	require.Equal(t, slowWritePOM, readAndClosePreparedTestBody(t, other.Body))
	require.Equal(t, pathsWithPendingFill, preparedResponseReplayPaths(t), "overload replay should be removed after opening the response")

	joined, err := service.responseFromFetched(context.Background(), Request{Method: http.MethodGet}, firstRoute, testFetchedPOM(io.NopCloser(strings.NewReader(slowWritePOM))))
	require.NoError(t, err)
	require.Equal(t, slowWritePOM, readAndClosePreparedTestBody(t, joined.Body))
	require.Equal(t, pathsWithPendingFill, preparedResponseReplayPaths(t), "same-key join should remove its redundant replay")
	require.NoError(t, first.Body.Close())
	require.NoError(t, service.Close())
	require.Equal(t, baselinePaths, preparedResponseReplayPaths(t), "completed fill should remove its replay")
}

type cancelOnCloseMavenBody struct {
	Reader    io.Reader
	cancel    context.CancelFunc
	readBytes int
	closed    bool
}

func (b *cancelOnCloseMavenBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.readBytes += n
	return n, err //nolint:wrapcheck // io.Reader must preserve the EOF sentinel.
}

func (b *cancelOnCloseMavenBody) Close() error {
	b.closed = true
	b.cancel()
	return nil
}

type countingMavenObjectStore struct {
	object.Store
	puts atomic.Int32
}

func (s *countingMavenObjectStore) Put(ctx context.Context, key string, body io.Reader, opts object.PutOptions) (*object.Info, error) {
	s.puts.Add(1)
	info, err := s.Store.Put(ctx, key, body, opts)
	return info, wrapError(err, "write counted maven object")
}

func newMavenObjectStore(t *testing.T) object.Store {
	t.Helper()
	store, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	return store
}

func newPreparedResponseTestService(t *testing.T, objects object.Store) *Service {
	t.Helper()
	service, metadata := newCacheDegradationService(t)
	service.cache = artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	service.fills = service.cache.FillTracker()
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	return service
}

func testPreparedPOMRoute(reference string) Route {
	return Route{Alias: "central", Repository: "com/acme/demo/1.0", Reference: reference + ".pom"}
}

func testFetchedPOM(body io.ReadCloser) *upstreamFetch {
	return &upstreamFetch{status: http.StatusOK, body: body}
}

func readPreparedTestBody(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	bytes, err := io.ReadAll(body)
	require.NoError(t, err)
	return string(bytes)
}

func readAndClosePreparedTestBody(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	value := readPreparedTestBody(t, body)
	require.NoError(t, body.Close())
	return value
}

func preparedResponseReplayPaths(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	require.NoError(t, err)
	var paths []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "regimux-maven-replay-") {
			paths = append(paths, entry.Name())
		}
	}
	slices.Sort(paths)
	return paths
}

func waitPreparedResponseSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("maven cache write did not start before timeout")
	}
}

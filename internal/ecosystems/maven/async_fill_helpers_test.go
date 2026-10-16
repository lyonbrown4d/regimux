//nolint:testpackage // Shared fixtures exercise private Maven fill and replay ownership.
package maven

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

func assertAsyncMavenGetResult(t *testing.T, response *Response, err error, wantErr bool) {
	t.Helper()
	if !wantErr {
		assertAsyncMavenResponse(t, response, err, asyncFillSecondPOM)
		return
	}
	if response != nil {
		closeReadCloser(response.Body, nil, "close rejected response")
	}
	require.Nil(t, response)
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, context.Canceled)
}

func newAsyncTwoArtifactUpstream(t *testing.T, secondBody string, firstCalls, secondCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return newAsyncUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		var body string
		switch r.URL.Path {
		case "/" + asyncFillFirstTail:
			firstCalls.Add(1)
			body = slowWritePOM
		case "/" + asyncFillSecondTail:
			secondCalls.Add(1)
			body = secondBody
		default:
			http.NotFound(w, r)
			return
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write upstream POM: %v", err)
		}
	})
}

type countingBlockingMavenWriteStore struct {
	*blockingMavenWriteStore
	puts        atomic.Int32
	releaseOnce sync.Once
}

func (s *countingBlockingMavenWriteStore) Put(ctx context.Context, key string, body io.Reader, opts object.PutOptions) (*object.Info, error) {
	s.puts.Add(1)
	info, err := s.blockingMavenWriteStore.Put(ctx, key, body, opts)
	return info, wrapError(err, "write counted Maven test object")
}

func (s *countingBlockingMavenWriteStore) releasePut() { s.releaseOnce.Do(func() { close(s.release) }) }

func newAsyncBlockingStore(t *testing.T) (object.Store, *countingBlockingMavenWriteStore) {
	t.Helper()
	local, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open objects: %v", err)
	}
	return local, &countingBlockingMavenWriteStore{blockingMavenWriteStore: &blockingMavenWriteStore{
		Store: local, entered: make(chan struct{}), release: make(chan struct{}),
	}}
}

func newAsyncFillService(t *testing.T, upstreamURL string, objects object.Store, fills *artifactcache.FillTracker) (*Service, meta.Store) {
	t.Helper()
	_, metadata := newCacheDegradationService(t)
	cache := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects, Fills: fills})
	service := NewService(ServiceDependencies{
		Config: config.Config{Maven: map[string]config.DependencyUpstreamConfig{"central": {Registry: upstreamURL}}},
		Cache:  cache, Metadata: metadata, Objects: objects,
	})
	t.Cleanup(func() {
		if closeErr := service.Close(); closeErr != nil {
			t.Errorf("close Maven service: %v", closeErr)
		}
	})
	return service, metadata
}

func newAsyncUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func assertAsyncMavenResponse(t *testing.T, response *Response, err error, want string) {
	t.Helper()
	if err != nil || response == nil || response.Body == nil || response.Status != http.StatusOK {
		t.Fatalf("Maven Get response = %#v, error=%v; want 200 with body", response, err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || string(body) != want {
		t.Fatalf("Maven body = %q, read err=%v, close err=%v; want %q", body, readErr, closeErr, want)
	}
}

func waitAsyncFillSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func assertAsyncMavenCache(t *testing.T, metadata meta.Store, objects object.Store, key meta.TagKey, want string) {
	t.Helper()
	tag, ok, err := metadata.Tag(t.Context(), key)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for !ok {
		require.NoError(t, err)
		select {
		case <-ticker.C:
			tag, ok, err = metadata.Tag(t.Context(), key)
		case <-deadline.C:
			t.Fatal("background Maven cache write did not commit")
		}
	}
	require.NoError(t, err)
	body, _, err := objects.Get(t.Context(), tag.Digest, object.GetOptions{})
	if err != nil {
		t.Fatalf("get committed object: %v", err)
	}
	data, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || string(data) != want {
		t.Fatalf("committed object = %q, read err=%v, close err=%v; want %q", data, readErr, closeErr, want)
	}
}

type renewalTestLocker struct {
	lease    *renewalTestLease
	acquired chan struct{}
}

type renewalTestLease struct {
	extended               chan struct{}
	released               chan struct{}
	commitVisible          func() bool
	commitVisibleAtRelease bool
	once                   sync.Once
}

func newRenewalTestLocker() *renewalTestLocker {
	return &renewalTestLocker{lease: &renewalTestLease{
		extended: make(chan struct{}, 8), released: make(chan struct{}),
	}, acquired: make(chan struct{})}
}

func (l *renewalTestLocker) AcquireLease(ctx context.Context, _ string, _ time.Duration) (artifactcache.FillLease, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, wrapError(err, "acquire test Maven fill lease")
	}
	close(l.acquired)
	return l.lease, true, nil
}

func (l *renewalTestLease) Extend(ctx context.Context, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return wrapError(err, "extend test Maven fill lease")
	}
	if ttl != 40*time.Millisecond {
		return errors.New("lease extension did not preserve the active test lease")
	}
	select {
	case <-l.released:
		return errors.New("released lease was extended")
	default:
	}
	select {
	case l.extended <- struct{}{}:
	default:
	}
	return nil
}

func (l *renewalTestLease) Release(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return wrapError(err, "release test Maven fill lease")
	}
	l.once.Do(func() {
		l.commitVisibleAtRelease = l.commitVisible != nil && l.commitVisible()
		close(l.released)
	})
	return nil
}

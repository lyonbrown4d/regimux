//nolint:testpackage // Regression exercises the validated replay handoff directly.
package maven

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

const slowWritePOM = `<project><modelVersion>4.0.0</modelVersion><groupId>com.acme</groupId><artifactId>demo</artifactId><version>1.0</version></project>`

type blockingMavenWriteStore struct {
	object.Store
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingMavenWriteStore) Put(ctx context.Context, key string, body io.Reader, opts object.PutOptions) (*object.Info, error) {
	s.once.Do(func() { close(s.entered) })
	select {
	case <-ctx.Done():
		return nil, wrapError(ctx.Err(), "wait for test cache write")
	case <-s.release:
		info, err := s.Store.Put(ctx, key, body, opts)
		return info, wrapError(err, "write released test cache object")
	}
}

func TestReadyMavenBodyDoesNotWaitForCacheWrite(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	response, err := service.responseFromFetched(ctx, Request{Method: http.MethodGet}, Route{
		Alias: "central", Repository: "com/acme/demo/1.0", Reference: "demo-1.0.pom",
	}, &upstreamFetch{
		status: http.StatusOK, headers: http.Header{"Content-Type": []string{"application/xml"}},
		body: io.NopCloser(strings.NewReader(slowWritePOM)),
	})
	require.NoError(t, err, "validated POM delivery must not wait for cache write")
	require.NotNil(t, response)
	require.NotNil(t, response.Body)
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, slowWritePOM, string(body))
	require.NoError(t, ctx.Err(), "cache write must not exhaust caller deadline")
	select {
	case <-objects.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("cache write was never attempted")
	}
}

package object_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

const s3PoolWaveRequests = 64

type s3PoolReadWave struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newS3PoolReadWave() *s3PoolReadWave {
	return &s3PoolReadWave{entered: make(chan struct{}, s3PoolWaveRequests), release: make(chan struct{})}
}

func (w *s3PoolReadWave) unblock() { w.once.Do(func() { close(w.release) }) }

func TestS3NativeClientReusesConnectionsBetweenConcurrentWaves(t *testing.T) {
	waves := []*s3PoolReadWave{newS3PoolReadWave(), newS3PoolReadWave()}
	var requests, connections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := (requests.Add(1) - 1) / s3PoolWaveRequests
		if r.Method != http.MethodHead || index >= int64(len(waves)) {
			t.Errorf("unexpected native S3 request: method=%s wave=%d", r.Method, index)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		wave := waves[index]
		wave.entered <- struct{}{}
		select {
		case <-wave.release:
			w.Header().Set("Content-Length", "7")
			w.Header().Set("ETag", `"pooled-object"`)
		case <-r.Context().Done():
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		for _, wave := range waves {
			wave.unblock()
		}
	})
	store, err := object.NewS3(t.Context(), object.S3Options{
		Bucket: "bucket", Endpoint: server.URL, ForcePathStyle: true,
		AccessKeyID: "fixture", SecretAccessKey: "fixture",
	})
	require.NoError(t, err)
	runS3PoolReadWave(t, store, waves[0])
	firstConnections := connections.Load()
	require.Equal(t, int64(s3PoolWaveRequests), firstConnections)
	runS3PoolReadWave(t, store, waves[1])
	require.Equal(t, firstConnections, connections.Load(), "the completed first wave must remain reusable instead of forcing fresh dials")
	require.Equal(t, int64(2*s3PoolWaveRequests), requests.Load())
}

func runS3PoolReadWave(t *testing.T, store *object.S3Store, wave *s3PoolReadWave) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, s3PoolWaveRequests)
	for range s3PoolWaveRequests {
		go func() {
			done <- readS3PoolObject(ctx, store)
		}()
	}
	for range s3PoolWaveRequests {
		select {
		case <-wave.entered:
		case <-ctx.Done():
			t.Fatal("native S3 requests did not overlap in one connection wave")
		}
	}
	wave.unblock()
	for range s3PoolWaveRequests {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("native S3 connection wave did not finish")
		}
	}
}

func readS3PoolObject(ctx context.Context, store *object.S3Store) error {
	info, err := store.Stat(ctx, ocidigest.FromString("payload").String())
	if err != nil {
		return fmt.Errorf("read pooled S3 object: %w", err)
	}
	if info == nil || info.Size != 7 || info.ETag != "pooled-object" {
		return fmt.Errorf("native S3 HEAD returned unexpected object info: %#v", info)
	}
	return nil
}

package cache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestConfiguredBlobOpenTimeoutKeepsSlowCacheAvailable(t *testing.T) {
	for _, tc := range []struct {
		name         string
		resume       bool
		rangeRequest *object.HTTPRange
		want         []byte
	}{
		{name: "initial full open", want: []byte("abcdef")},
		{name: "initial range open", rangeRequest: &object.HTTPRange{Start: 2, End: 4}, want: []byte("cde")},
		{name: "continuation open", resume: true, want: []byte("abcdef")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata, objects := newTestStores(t)
			payload := []byte("abcdef")
			digest := testDigestFor(payload)
			seedStoredBlob(t, metadata, objects, digest, payload)
			var upstreamCalls atomic.Int32
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer registry.Close()
			client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
			delayed := &configuredSlowOpenStore{Store: objects, resume: tc.resume, payload: payload}
			proxy := newTestProxy(client, metadata, delayed, nil, configuredOpenTimeout(t))
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			response, err := proxy.Blobs().Get(ctx, cache.BlobRequest{
				UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet, Range: tc.rangeRequest,
			})
			require.NoError(t, err, "healthy slow cache must not depend on unavailable origin")
			require.Equal(t, cache.CacheHit, response.Cache)
			body, readErr := io.ReadAll(response.Reader)
			require.NoError(t, response.Reader.Close())
			require.NoError(t, readErr)
			require.Equal(t, tc.want, body)
			require.Zero(t, upstreamCalls.Load())
			wantOpens := int32(1)
			if tc.resume {
				wantOpens = 2
			}
			require.Equal(t, wantOpens, delayed.calls.Load())
		})
	}
}

func configuredOpenTimeout(t *testing.T) config.Config {
	t.Helper()
	var cfg config.Config
	require.NoError(t, json.Unmarshal([]byte(`{"cache":{"blob":{"open_timeout":3000000000}}}`), &cfg))
	return cfg
}

type configuredSlowOpenStore struct {
	object.Store
	resume  bool
	payload []byte
	calls   atomic.Int32
	entered chan struct{}
}

func (s *configuredSlowOpenStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	first := s.calls.Add(1) == 1
	if first && s.entered != nil {
		close(s.entered)
	}
	if !s.resume || !first {
		if err := waitConfiguredSlowOpen(ctx); err != nil {
			return nil, nil, err
		}
	}
	body, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return body, info, fmt.Errorf("open fixture object: %w", err)
	}
	if !s.resume || !first {
		return body, info, nil
	}
	if err := body.Close(); err != nil {
		return nil, nil, fmt.Errorf("close original fixture body: %w", err)
	}
	return io.NopCloser(bytes.NewReader(s.payload[:2])), info, nil
}

func waitConfiguredSlowOpen(ctx context.Context) error {
	timer := time.NewTimer(2200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("configured slow cache: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func TestConfiguredBlobOpenTimeoutHonorsCaller(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancellation"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			metadata, objects := newTestStores(t)
			payload := []byte("abcdef")
			digest := testDigestFor(payload)
			seedStoredBlob(t, metadata, objects, digest, payload)
			var originCalls atomic.Int32
			registry := unavailableConfiguredOrigin(t, &originCalls)
			client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
			store := &configuredSlowOpenStore{Store: objects, entered: make(chan struct{})}
			proxy := newTestProxy(client, metadata, store, nil, configuredOpenTimeout(t))
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if !deadline {
				go cancelConfiguredOpen(ctx, store.entered, cancel)
			}
			_, err := proxy.Blobs().Get(ctx, cache.BlobRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet})
			want := context.Canceled
			if deadline {
				want = context.DeadlineExceeded
			}
			require.ErrorIs(t, err, want)
			require.False(t, errors.Is(err, transfer.ErrObjectOpenTimeout))
			require.Equal(t, int32(1), store.calls.Load())
			require.Zero(t, originCalls.Load())
		})
	}
}

func cancelConfiguredOpen(ctx context.Context, entered <-chan struct{}, cancel context.CancelFunc) {
	select {
	case <-entered:
		cancel()
	case <-ctx.Done():
	}
}

func TestConfiguredBlobOpenTimeoutConcurrentSlowHits(t *testing.T) {
	metadata, objects := newTestStores(t)
	payload := []byte("abcdef")
	digest := testDigestFor(payload)
	seedStoredBlob(t, metadata, objects, digest, payload)
	var originCalls atomic.Int32
	registry := unavailableConfiguredOrigin(t, &originCalls)
	client := newCacheTestUpstreamClient(map[string]upstream.Config{"hub": {Registry: registry.URL}})
	store := &configuredSlowOpenStore{Store: objects}
	proxy := newTestProxy(client, metadata, store, nil, configuredOpenTimeout(t))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(32)
	for range 32 {
		group.Go(func() error {
			return checkConfiguredSlowHit(ctx, proxy, digest, payload)
		})
	}
	require.NoError(t, group.Wait())
	require.Equal(t, int32(32), store.calls.Load())
	require.Zero(t, originCalls.Load())
}

func checkConfiguredSlowHit(ctx context.Context, proxy *cache.Proxy, digest string, payload []byte) error {
	response, err := proxy.Blobs().Get(ctx, cache.BlobRequest{UpstreamAlias: "hub", Repo: "library/alpine", Digest: digest, Method: http.MethodGet})
	if err != nil {
		return fmt.Errorf("concurrent slow hit: %w", err)
	}
	body, readErr := io.ReadAll(response.Reader)
	closeErr := response.Reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return fmt.Errorf("read concurrent hit: %w", err)
	}
	if !bytes.Equal(body, payload) || response.Cache != cache.CacheHit {
		return errors.New("concurrent hit body/cache mismatch")
	}
	return nil
}

func unavailableConfiguredOrigin(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	return server
}

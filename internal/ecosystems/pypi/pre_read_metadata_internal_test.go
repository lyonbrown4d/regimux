package pypi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/stretchr/testify/require"
)

func TestPackageBodyReadSurvivesSlowSynchronousPullMetadata(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "metadata.db")})
		require.NoError(t, err)
		t.Cleanup(func() {
			closeErr := db.Close()
			require.NoError(t, closeErr)
		})
		metadata := &slowPullMetadata{Store: db}
		var calls atomic.Int32
		client := &http.Client{Transport: fillTransport(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Length": {"10"}, "Etag": {`"wheel-v1"`}},
				Body:       &metadataDelayBody{ctx: req.Context()},
			}, nil
		})}
		service := NewService(ServiceDependencies{
			Config: config.Config{PyPI: config.DependencyEcosystemConfig{
				"pypi": {Registry: "https://index.test"},
			}},
			Metadata: metadata,
			Client:   client,
		})

		resp, err := service.Get(ctx, Request{
			Alias: "pypi",
			Tail:  "packages/https/index.test/packages/demo-1.0-py3-none-any.whl",
		})
		require.NoError(t, err)
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		require.NoError(t, closeErr)
		require.NoError(t, readErr, "read package body after metadata writes")
		require.Equal(t, "0123456789", string(body), "complete upstream body")
		require.EqualValues(t, 1, calls.Load(), "one upstream call after delayed metadata writes")
		require.EqualValues(t, 1, metadata.proxyPulls.Load(), "completed proxy pull write")
		require.EqualValues(t, 1, metadata.upstreamPulls.Load(), "completed upstream pull write")

		route, err := ParseTail("pypi", "packages/https/index.test/packages/demo-1.0-py3-none-any.whl")
		require.NoError(t, err)
		key := pypiPullKey(route)
		pull, found, err := db.Pull(ctx, key)
		require.NoError(t, err)
		require.True(t, found, "persisted pull record")
		require.NotNil(t, pull)
		require.False(t, pull.LastUpstreamPullAt.IsZero(), "upstream pull write persisted")
	})
}

func TestSlowPullMetadataDoesNotExpireUnreadResponseBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "metadata.db")})
		require.NoError(t, err)
		t.Cleanup(func() {
			closeErr := db.Close()
			require.NoError(t, closeErr)
		})
		metadata := &blockedPullMetadata{Store: db}
		var calls atomic.Int32
		client := &http.Client{Transport: fillTransport(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Length": {"10"}, "Etag": {`"wheel-v1"`}},
				Body:       &metadataDelayBody{ctx: req.Context()},
			}, nil
		})}
		service := NewService(ServiceDependencies{
			Config: config.Config{PyPI: config.DependencyEcosystemConfig{
				"pypi": {Registry: "https://index.test"},
			}},
			Metadata: metadata,
			Client:   client,
		})

		resp, err := service.Get(ctx, Request{
			Alias: "pypi",
			Tail:  "packages/https/index.test/packages/demo-1.0-py3-none-any.whl",
		})
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(packageReadLease - time.Second)
		synctest.Wait()
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		require.NoError(t, closeErr)
		require.NoError(t, readErr, "read body after metadata timeout")
		require.Equal(t, "0123456789", string(body), "complete upstream body")
		require.EqualValues(t, 1, calls.Load(), "one upstream call")
		require.EqualValues(t, 1, metadata.recordPullCalls.Load(), "synchronous proxy pull attempt completed")
		require.EqualValues(t, 1, metadata.upstreamPullCalls.Load(), "synchronous upstream pull attempt completed")
	})
}

type blockedPullMetadata struct {
	meta.Store
	recordPullCalls   atomic.Int32
	upstreamPullCalls atomic.Int32
}

func (m *blockedPullMetadata) RecordPull(ctx context.Context, _ meta.PullKey, _ time.Time) (*meta.PullRecord, error) {
	m.recordPullCalls.Add(1)
	<-ctx.Done()
	return nil, fmt.Errorf("wait for proxy pull write: %w", ctx.Err())
}

func (m *blockedPullMetadata) RecordUpstreamPull(ctx context.Context, _ meta.PullKey, _ time.Time) (*meta.PullRecord, error) {
	m.upstreamPullCalls.Add(1)
	return nil, fmt.Errorf("record upstream pull: %w", ctx.Err())
}

type slowPullMetadata struct {
	meta.Store
	proxyPulls    atomic.Int32
	upstreamPulls atomic.Int32
}

func (m *slowPullMetadata) RecordPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	time.Sleep(11 * time.Second)
	record, err := m.Store.RecordPull(ctx, key, at)
	if err != nil {
		return nil, fmt.Errorf("record proxy pull: %w", err)
	}
	m.proxyPulls.Add(1)
	return record, nil
}

func (m *slowPullMetadata) RecordUpstreamPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	time.Sleep(11 * time.Second)
	record, err := m.Store.RecordUpstreamPull(ctx, key, at)
	if err != nil {
		return nil, fmt.Errorf("record upstream pull: %w", err)
	}
	m.upstreamPulls.Add(1)
	return record, nil
}

type metadataDelayBody struct {
	ctx  context.Context
	done bool
}

func (b *metadataDelayBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, fmt.Errorf("upstream body request canceled: %w", err)
	}
	if b.done {
		return 0, io.EOF
	}
	b.done = true
	return copy(p, "0123456789"), io.EOF
}

func (b *metadataDelayBody) Close() error { return nil }

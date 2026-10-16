package artifactcache_test

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

func TestGetAfterPutDoesNotJoinEarlierSnapshot(t *testing.T) {
	metadata, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "meta.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metadata.Close()) })
	objects, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	blocked := &heldTagRead{MetadataStore: metadata, started: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(blocked.unblock)
	store := artifactcache.New(artifactcache.Dependencies{Metadata: blocked, Objects: objects})
	key := artifactcache.Key{Alias: "npm", Repository: "demo", Reference: "metadata"}
	putReadVersion(t, store, key, "old version")
	oldResult := make(chan readResult, 1)
	go func() {
		entry, _, readErr := store.Get(t.Context(), key)
		oldResult <- readResult{entry: entry, err: readErr}
	}()
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first read did not capture its tag snapshot")
	}
	putReadVersion(t, store, key, "new version")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	entry, found, err := store.Get(ctx, key)
	require.NoError(t, err, "read started after Put must not wait for the old snapshot")
	require.True(t, found)
	assertReadVersion(t, entry, "new version")
	blocked.unblock()
	previous := <-oldResult
	require.NoError(t, previous.err)
	assertReadVersion(t, previous.entry, "old version")
}

func putReadVersion(t *testing.T, store *artifactcache.Store, key artifactcache.Key, body string) {
	t.Helper()
	entry, err := store.Put(t.Context(), artifactcache.PutRequest{
		Key: key, Body: strings.NewReader(body), ContentType: "text/plain",
	})
	require.NoError(t, err)
	require.NoError(t, entry.Body.Close())
}

func assertReadVersion(t *testing.T, entry artifactcache.Entry, expected string) {
	t.Helper()
	body, err := io.ReadAll(entry.Body)
	closeErr := entry.Body.Close()
	require.NoError(t, err)
	require.NoError(t, closeErr)
	require.Equal(t, expected, string(body))
}

type heldTagRead struct {
	artifactcache.MetadataStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   atomic.Int64
}

func (m *heldTagRead) unblock() { m.once.Do(func() { close(m.release) }) }

func (m *heldTagRead) Tag(ctx context.Context, key meta.TagKey) (*meta.TagRecord, bool, error) {
	tag, found, err := m.MetadataStore.Tag(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read held tag: %w", err)
	}
	if m.calls.Add(1) != 1 {
		return tag, found, nil
	}
	close(m.started)
	select {
	case <-m.release:
		return tag, found, nil
	case <-ctx.Done():
		return nil, false, fmt.Errorf("wait for held tag: %w", ctx.Err())
	}
}

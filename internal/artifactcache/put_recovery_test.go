package artifactcache_test

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

func TestCachedBodyRecoveryAfterInterruptedObjectRead(t *testing.T) {
	metadata, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "meta.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metadata.Close()) })
	local, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	objects := &interruptedPutRead{Store: local}
	store := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	key := artifactcache.Key{Alias: "maven", Repository: "demo", Reference: "demo.jar"}
	stored, err := store.Put(t.Context(), artifactcache.PutRequest{
		Key:  key,
		Body: strings.NewReader("0123456789"), ContentType: "application/octet-stream",
	})
	require.NoError(t, err)
	require.NoError(t, stored.Body.Close())
	require.Zero(t, objects.reads, "a successful fill returns its local verified spool")
	entry, found, err := store.Get(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	body, err := io.ReadAll(entry.Body)
	require.NoError(t, err)
	require.NoError(t, entry.Body.Close())
	require.Equal(t, "0123456789", string(body))
	require.Equal(t, 2, objects.reads)
}

type interruptedPutRead struct {
	object.Store
	reads int
}

func (s *interruptedPutRead) Get(ctx context.Context, key string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	s.reads++
	body, info, err := s.Store.Get(ctx, key, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("read interrupted object fixture: %w", err)
	}
	if s.reads == 1 {
		return &limitedPutRead{Reader: io.LimitReader(body, 4), Closer: body}, info, nil
	}
	if opts.Range == nil || opts.Range.Start != 4 || opts.Range.End != 9 {
		return nil, nil, fmt.Errorf("unexpected recovery range: %v", opts.Range)
	}
	return body, info, nil
}

type limitedPutRead struct {
	io.Reader
	io.Closer
}

package cachegc_test

import (
	"io"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cachegc"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/stretchr/testify/require"
)

func TestCleanupEvictsDiskCacheWithCanonicalObject(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "canonical object exists"
		if missing {
			name = "canonical object already removed"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			metadata, backend := testStores(t)
			now := time.Now().UTC().Add(72 * time.Hour)
			body := []byte("old complete cached artifact")
			digest := putBlob(ctx, t, metadata, backend, body, now.Add(-72*time.Hour))
			cache, err := object.NewDiskCache(backend, object.DiskCacheOptions{
				Directory: t.TempDir(), MaxBytes: 2 << 20, MaxObjectBytes: 1 << 20,
				MaxEntries: 10, MaxConcurrentFills: 2, TTL: 24 * time.Hour,
			})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, cache.Close()) })
			reader, _, err := cache.Get(ctx, digest, object.GetOptions{})
			require.NoError(t, err)
			got, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			require.Equal(t, body, got)
			require.Equal(t, 1, cache.Stats().Entries)
			if missing {
				require.NoError(t, backend.Delete(ctx, digest))
			}

			report, err := cachegc.New(metadata, cache).Cleanup(ctx, cachegc.CleanupOptions{
				UnusedFor: 24 * time.Hour, PruneReferences: true, Now: now,
			})
			require.NoError(t, err)
			require.Equal(t, 1, report.DeletedBlobs)
			wantMissing := 0
			if missing {
				wantMissing = 1
			}
			require.Equal(t, wantMissing, report.MissingObjects)
			require.Zero(t, cache.Stats().Entries)
			assertMissing(ctx, t, backend, digest)
			_, _, err = cache.Get(ctx, digest, object.GetOptions{})
			require.ErrorIs(t, err, object.ErrNotFound)
		})
	}
}

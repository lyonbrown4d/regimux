package artifactcache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestManifestSharesGetFlightWithoutOpeningAnotherBody(t *testing.T) {
	now := time.Now().UTC()
	metadata := newReadMetadata(now.Add(-time.Hour), []byte("complete artifact body"))
	objects := &readObjectStore{body: []byte("complete artifact body")}
	store := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata, Objects: objects, Now: func() time.Time { return now },
	})
	key := artifactcache.Key{Alias: "pypi", Repository: "packages/demo", Reference: "1.2"}
	owner := make(chan readResult, 1)
	go func() {
		entry, _, err := store.Get(t.Context(), key)
		owner <- readResult{entry: entry, err: err}
	}()
	select {
	case <-metadata.tagStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not enter metadata read")
	}
	var ready sync.WaitGroup
	ready.Add(1)
	follower := make(chan manifestReadResult, 1)
	go func() {
		ctx := &readWaitContext{Context: t.Context(), ready: &ready}
		manifest, found, err := artifactcache.LookupManifest(ctx, store, key)
		follower <- manifestReadResult{manifest: manifest, found: found, err: err}
	}()
	ready.Wait()
	close(metadata.releaseTag)
	assertReadResult(t, <-owner, objects.body)
	assertManifestRead(t, <-follower)
	metadata.assertReadCounts(t, 1)
	require.Equal(t, int64(1), objects.gets.Load(), "metadata read must not open an object body")
	manifest, found, err := artifactcache.LookupManifest(t.Context(), store, key)
	assertManifestRead(t, manifestReadResult{manifest: manifest, found: found, err: err})
	metadata.assertReadCounts(t, 2)
	require.Equal(t, int64(1), objects.gets.Load())
}

type manifestReadResult struct {
	manifest *meta.ManifestRecord
	found    bool
	err      error
}

func assertManifestRead(t *testing.T, result manifestReadResult) {
	t.Helper()
	require.NoError(t, result.err)
	require.True(t, result.found)
	require.NotNil(t, result.manifest)
	require.Equal(t, ocidigest.FromString("complete artifact body").String(), result.manifest.Digest)
	require.Equal(t, []string{"first", "second"}, result.manifest.Headers["X-Test"])
	result.manifest.Headers["X-Test"][0] = "metadata caller mutation"
}

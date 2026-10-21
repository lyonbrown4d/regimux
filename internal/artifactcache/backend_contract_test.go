package artifactcache_test

import (
	"testing"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/cache/backend"
)

func TestProductionKVSupportsDistributedArtifactFills(t *testing.T) {
	var cacheBackend backend.Backend = (*backend.KV)(nil)
	if _, ok := cacheBackend.(artifactcache.FillLocker); !ok {
		t.Fatal("production KV backend does not satisfy the artifact fill lease contract")
	}
}

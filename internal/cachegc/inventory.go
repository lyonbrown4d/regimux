package cachegc

import (
	"context"
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

// LoadCleanupInventory reads the shared metadata once for all ecosystem SPIs.
func LoadCleanupInventory(ctx context.Context, store meta.Store) (ecosystem.CacheCleanupInventory, error) {
	snapshot, err := meta.ReadCleanupSnapshot(ctx, store)
	if err != nil {
		return ecosystem.CacheCleanupInventory{}, fmt.Errorf("load cleanup inventory: %w", err)
	}
	return ecosystem.CacheCleanupInventory{
		Manifests: snapshot.Manifests,
		Tags:      snapshot.Tags,
		Blobs:     snapshot.Blobs,
		RepoBlobs: snapshot.RepoBlobs,
		Pulls:     snapshot.Pulls,
	}, nil
}

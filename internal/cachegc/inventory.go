package cachegc

import (
	"context"
	"fmt"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

// LoadCleanupInventory reads the shared metadata once for all ecosystem SPIs.
func LoadCleanupInventory(ctx context.Context, store meta.Store) (ecosystem.CacheCleanupInventory, error) {
	var out ecosystem.CacheCleanupInventory
	manifests, err := store.ListManifests(ctx)
	if err != nil {
		return out, fmt.Errorf("load cleanup manifests: %w", err)
	}
	out.Manifests = cleanupRows(manifests)
	tags, err := store.ListTags(ctx)
	if err != nil {
		return out, fmt.Errorf("load cleanup tags: %w", err)
	}
	out.Tags = cleanupRows(tags)
	blobs, err := store.ListBlobs(ctx)
	if err != nil {
		return out, fmt.Errorf("load cleanup blobs: %w", err)
	}
	out.Blobs = cleanupRows(blobs)
	links, err := store.ListRepoBlobs(ctx)
	if err != nil {
		return out, fmt.Errorf("load cleanup repository links: %w", err)
	}
	out.RepoBlobs = cleanupRows(links)
	pulls, err := store.ListPulls(ctx)
	if err != nil {
		return out, fmt.Errorf("load cleanup activity: %w", err)
	}
	out.Pulls = cleanupRows(pulls)
	return out, nil
}

func cleanupRows[T any](rows *collectionlist.List[T]) []T {
	if rows == nil {
		return nil
	}
	return rows.Values()
}

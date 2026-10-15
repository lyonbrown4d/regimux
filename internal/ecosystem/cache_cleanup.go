package ecosystem

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

// CacheCleanupInventory is a single shared snapshot used by ecosystem planners.
type CacheCleanupInventory struct {
	Manifests []meta.ManifestRecord
	Tags      []meta.TagRecord
	Blobs     []meta.BlobRecord
	RepoBlobs []meta.RepoBlobRecord
	Pulls     []meta.PullRecord
}

type CacheCleanupOptions struct {
	Now        time.Time
	UnusedFor  time.Duration
	MaxScan    int
	MaxDeletes int
	DryRun     bool
}

// CacheCleanupGroup is an ecosystem's complete logical cache eviction unit.
// A group may share manifests or digests with other groups and ecosystems.
type CacheCleanupGroup struct {
	Ecosystem    string
	Alias        string
	Package      string
	LastAccessAt time.Time
	Manifests    []meta.ManifestRecord
	Tags         []meta.TagRecord
	RepoBlobs    []meta.RepoBlobRecord
	Digests      []string
}

// CacheCleanupPlanner owns package boundaries and retention policy for an ecosystem.
// The scheduler applies the combined plans before reclaiming unreferenced CAS objects.
type CacheCleanupPlanner interface {
	Runtime
	PlanCacheCleanup(context.Context, CacheCleanupInventory, CacheCleanupOptions) ([]CacheCleanupGroup, error)
}

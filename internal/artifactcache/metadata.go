package artifactcache

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

// LookupMetadata coalesces a tag, manifest, and access touch without opening a body.
// Callers own the returned values and manifest headers, and evaluate expiry themselves.
func LookupMetadata(ctx context.Context, s *Store, key Key) (meta.TagRecord, meta.ManifestRecord, bool, error) {
	snapshot, err := s.readCachedMetadata(ctx, key)
	if err != nil || !snapshot.found {
		return meta.TagRecord{}, meta.ManifestRecord{}, false, err
	}
	return snapshot.tag, cloneManifestHeaders(snapshot.manifest), true, nil
}

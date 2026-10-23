package artifactcache

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (s *Store) touchAccess(ctx context.Context, manifest meta.ManifestRecord, at time.Time) {
	if s.submitAccess == nil {
		s.touchAccessNow(ctx, manifest, at)
		return
	}
	if ctx.Err() != nil {
		return
	}
	fill, owner := s.accesses.Begin(manifest.Digest)
	if !owner {
		return
	}
	// Retain only immutable blob identity, never the response headers or body.
	access := meta.ManifestRecord{
		Digest: manifest.Digest, Size: manifest.Size,
		MediaType: manifest.MediaType, ObjectKey: manifest.ObjectKey,
	}
	// A completed read may outlive its caller for this bounded metadata update.
	touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accessTouchTimeout)
	err := s.submitAccess(func() {
		defer cancel()
		defer s.accesses.Finish(access.Digest, fill, nil)
		s.touchAccessNow(touchCtx, access, at)
	})
	if err != nil {
		cancel()
		s.accesses.Finish(access.Digest, fill, err)
		s.logger.DebugContext(ctx, "artifact access update skipped", "digest", access.Digest, "error", err)
	}
}

func (s *Store) touchAccessNow(ctx context.Context, manifest meta.ManifestRecord, at time.Time) {
	if err := Touch(ctx, s.metadata, manifest, at); err != nil {
		s.logger.WarnContext(ctx, "touch artifact cache access failed", "digest", manifest.Digest, "error", err)
	}
}

package cache

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func manifestForMethod(manifest *CachedManifest, method string) *CachedManifest {
	if method == http.MethodHead {
		manifest.Body = nil
	}
	manifest.Cache = CacheHit
	return manifest
}

func (p manifestProxy) touchManifestAccess(ctx context.Context, record meta.ManifestRecord) {
	if p.metadata == nil {
		return
	}
	if err := artifactcache.Touch(ctx, p.metadata, record, time.Now()); err != nil {
		slog.WarnContext(ctx, "touch container manifest cache access failed", "digest", record.Digest, "error", err)
	}
}

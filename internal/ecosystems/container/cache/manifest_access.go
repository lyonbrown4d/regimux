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
	if p.accessGroup == nil {
		p.logManifestAccessError(ctx, record, artifactcache.Touch(ctx, p.metadata, record, time.Now()))
		return
	}
	_, err, _ := p.accessGroup.DoContext(ctx, "oci-manifest-access:"+record.Digest, func(sharedCtx context.Context) (bool, error) {
		return true, artifactcache.Touch(sharedCtx, p.metadata, record, time.Now())
	})
	p.logManifestAccessError(ctx, record, err)
}

func (p manifestProxy) logManifestAccessError(ctx context.Context, record meta.ManifestRecord, err error) {
	if err != nil {
		slog.WarnContext(ctx, "touch container manifest cache access failed", "digest", record.Digest, "error", err)
	}
}

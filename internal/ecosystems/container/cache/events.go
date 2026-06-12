package cache

import (
	"context"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func publishCacheEvent(ctx context.Context, bus events.Bus, event events.Event) {
	if err := events.Publish(ctx, bus, event); err != nil {
		return
	}
}

func (p manifestProxy) publishCacheAccess(ctx context.Context, req ManifestRequest, result *CachedManifest) {
	if result == nil {
		return
	}
	publishCacheEvent(ctx, p.events, events.CacheAccess{
		Kind:       "manifest",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
		Digest:     result.Digest,
		Status:     string(result.Cache),
	})
}

func (p manifestProxy) publishArtifactPulled(ctx context.Context, req ManifestRequest, result *CachedManifest) {
	if result == nil {
		return
	}
	publishCacheEvent(ctx, p.events, events.ArtifactPulled{
		Ecosystem:  ecosystem.Container,
		Kind:       "manifest",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
		Status:     string(result.Cache),
		Accept:     reference.NormalizeAccept(req.Accept),
	})
}

func (p manifestProxy) publishDependencyPulled(ctx context.Context, req ManifestRequest, result *CachedManifest) {
	if result == nil {
		return
	}
	publishCacheEvent(ctx, p.events, events.DependencyPulled{
		Ecosystem:  ecosystem.Container,
		Kind:       "manifest",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
		Status:     string(result.Cache),
	})
}

func (p manifestProxy) publishCacheStore(ctx context.Context, req ManifestRequest, manifest *CachedManifest) {
	if manifest == nil {
		return
	}
	publishCacheEvent(ctx, p.events, events.CacheStore{
		Kind:       "manifest",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
		Digest:     manifest.Digest,
		Size:       manifest.Size,
	})
}

func (p blobProxy) publishCacheAccess(ctx context.Context, req BlobRequest, status CacheStatus) {
	publishCacheEvent(ctx, p.events, events.CacheAccess{
		Kind:       "blob",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
		Status:     string(status),
	})
}

func (p blobProxy) publishCacheStore(ctx context.Context, req BlobRequest, infoSize int64, digest string) {
	publishCacheEvent(ctx, p.events, events.CacheStore{
		Kind:       "blob",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     digest,
		Size:       infoSize,
	})
}

func (p tagProxy) publishCacheAccess(ctx context.Context, req TagRequest, status CacheStatus) {
	publishCacheEvent(ctx, p.events, events.CacheAccess{
		Kind:       "tags",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Status:     string(status),
	})
}

func (p tagProxy) publishCacheStore(ctx context.Context, req TagRequest, result *TagsResult) {
	if result == nil {
		return
	}
	publishCacheEvent(ctx, p.events, events.CacheStore{
		Kind:       "tags",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Size:       int64(len(result.Body)),
	})
}

func (p referrerProxy) publishCacheAccess(ctx context.Context, req ReferrerRequest, status CacheStatus) {
	publishCacheEvent(ctx, p.events, events.CacheAccess{
		Kind:       "referrers",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
		Status:     string(status),
	})
}

func (p referrerProxy) publishCacheStore(ctx context.Context, req ReferrerRequest, result *ReferrersResult) {
	if result == nil {
		return
	}
	publishCacheEvent(ctx, p.events, events.CacheStore{
		Kind:       "referrers",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
		Size:       int64(len(result.Body)),
	})
}

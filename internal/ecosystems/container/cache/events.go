package cache

import (
	"context"
	"errors"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func publishCacheEvent(ctx context.Context, bus events.Bus, event events.Event) {
	if err := events.Publish(ctx, bus, event); err != nil {
		return
	}
}

func publishContainerPullCacheAccess(ctx context.Context, bus events.Bus, kind, alias string, status CacheStatus) {
	publishCacheEvent(ctx, bus, events.ContainerPullCacheAccess{
		Kind:        kind,
		Alias:       alias,
		CacheStatus: string(status),
	})
}

func publishContainerPullStreamCacheFallback(ctx context.Context, bus events.Bus, alias, reason string) {
	publishCacheEvent(ctx, bus, events.ContainerPullStreamCacheFallback{
		Alias:  alias,
		Reason: normalizeContainerPullReason(reason),
	})
}

func publishContainerPullFill(ctx context.Context, bus events.Bus, alias, source, kind, status, reason string) {
	publishCacheEvent(ctx, bus, events.ContainerPullFill{
		Alias:  alias,
		Source: source,
		Kind:   kind,
		Status: status,
		Reason: normalizeContainerPullReason(reason),
	})
}

func (p manifestProxy) publishCacheAccess(ctx context.Context, req ManifestRequest, result *CachedManifest) {
	if result == nil {
		return
	}
	publishContainerPullCacheAccess(ctx, p.events, "manifest", req.UpstreamAlias, result.Cache)
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
	publishContainerPullCacheAccess(ctx, p.events, "blob", req.UpstreamAlias, status)
	publishCacheEvent(ctx, p.events, events.CacheAccess{
		Kind:       "blob",
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     req.Digest,
		Status:     string(status),
	})
}

func (p blobProxy) publishStreamCacheFallback(ctx context.Context, req BlobRequest, reason string) {
	publishContainerPullStreamCacheFallback(ctx, p.events, req.UpstreamAlias, reason)
}

func (p blobProxy) publishStreamCacheFill(ctx context.Context, req BlobRequest, status, reason string) {
	publishContainerPullFill(ctx, p.events, req.UpstreamAlias, "stream_cache", "blob", status, reason)
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
	publishContainerPullCacheAccess(ctx, p.events, "tags", req.UpstreamAlias, status)
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
	publishContainerPullCacheAccess(ctx, p.events, "referrers", req.UpstreamAlias, status)
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

func streamCacheFallbackReason(err error) string {
	switch {
	case errors.Is(err, errBlobStreamSchedulerSaturated):
		return "scheduler_saturated"
	case errors.Is(err, errBlobStreamSchedulerUnavailable):
		return "scheduler_unavailable"
	case err != nil:
		return "scheduler_submit_failed"
	default:
		return "unknown"
	}
}

func normalizeContainerPullReason(reason string) string {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return "unknown"
	case strings.HasPrefix(reason, "failure backoff until "):
		return "failure_backoff"
	}
	reason = strings.ToLower(reason)
	replacer := strings.NewReplacer(
		" ", "_",
		"-", "_",
		".", "_",
		"/", "_",
		":", "_",
	)
	reason = replacer.Replace(reason)
	out := make([]rune, 0, len(reason))
	lastUnderscore := false
	for _, r := range reason {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			out = append(out, r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			out = append(out, '_')
			lastUnderscore = true
		}
	}
	normalized := strings.Trim(string(out), "_")
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

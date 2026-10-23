package cache

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

const preparedManifestCommitTimeout = 90 * time.Second

type preparedManifestMetadata struct {
	once sync.Once
	err  error
	run  func(context.Context) error
}

func (m *preparedManifestMetadata) complete(ctx context.Context) error {
	m.once.Do(func() {
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), preparedManifestCommitTimeout)
		defer cancel()
		m.err = m.run(commitCtx)
	})
	return m.err
}

func newPreparedManifestMetadata(
	metadata meta.Store,
	logger *slog.Logger,
	pullKey meta.PullKey,
	upstreamPull bool,
	record *meta.ManifestRecord,
	tag *meta.TagRecord,
	propagateSaveError bool,
) *preparedManifestMetadata {
	if logger == nil {
		logger = slog.Default().With("component", "cache.proxy")
	}
	logger = logger.With(
		"upstream_alias", pullKey.Alias,
		"repository", pullKey.Repository,
		"reference", pullKey.Reference,
	)
	return &preparedManifestMetadata{run: func(ctx context.Context) error {
		if upstreamPull && metadata != nil {
			recordPreparedUpstreamPull(ctx, logger, metadata, pullKey)
		}
		return savePreparedCanonicalManifest(ctx, logger, metadata, record, tag, propagateSaveError)
	}}
}

func recordPreparedUpstreamPull(ctx context.Context, logger *slog.Logger, metadata meta.Store, key meta.PullKey) {
	if _, err := metadata.RecordUpstreamPull(ctx, key, time.Now().UTC()); err != nil {
		logger.WarnContext(ctx, "record prepared manifest upstream pull failed", "error", err)
	}
}

func savePreparedCanonicalManifest(
	ctx context.Context,
	logger *slog.Logger,
	metadata meta.Store,
	record *meta.ManifestRecord,
	tag *meta.TagRecord,
	propagateSaveError bool,
) error {
	if record == nil || record.Digest == "" || metadata == nil {
		return nil
	}
	if propagateSaveError {
		return savePreparedRevalidation(ctx, metadata, record, tag)
	}
	if _, err := metadata.UpsertManifest(ctx, *record); err != nil {
		logger.WarnContext(ctx, "upsert prepared manifest failed", "error", err)
		return nil
	}
	if tag == nil {
		return nil
	}
	if _, err := metadata.UpsertTag(ctx, *tag); err != nil {
		logger.WarnContext(ctx, "upsert prepared manifest tag failed", "error", err)
	}
	return nil
}

func savePreparedRevalidation(ctx context.Context, metadata meta.Store, record *meta.ManifestRecord, tag *meta.TagRecord) error {
	if _, err := metadata.UpsertManifest(ctx, *record); err != nil {
		return wrapError(err, "upsert revalidated manifest")
	}
	if tag == nil {
		return nil
	}
	if _, err := metadata.UpsertTag(ctx, *tag); err != nil {
		return wrapError(err, "upsert revalidated manifest tag")
	}
	return nil
}

func (p manifestProxy) Prepare(ctx context.Context, req ManifestRequest) (*CachedManifest, ManifestCompletion, error) {
	req.deferMetadata = true
	manifest, err := p.Get(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	compact := CachedManifest{
		Digest: manifest.Digest, MediaType: manifest.MediaType, Size: manifest.Size, Cache: manifest.Cache,
		preparedMetadata: manifest.preparedMetadata,
	}
	return manifest, &manifestResponseCompletion{proxy: p, req: req, compact: compact}, nil
}

type manifestResponseCompletion struct {
	proxy        manifestProxy
	req          ManifestRequest
	compact      CachedManifest
	responseOnce sync.Once
}

func (c *manifestResponseCompletion) Finalize(ctx context.Context) error {
	if c.compact.preparedMetadata == nil {
		return nil
	}
	return c.compact.preparedMetadata.complete(ctx)
}

func (c *manifestResponseCompletion) CompleteResponse(ctx context.Context) error {
	if err := c.Finalize(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return wrapError(err, "complete prepared manifest response")
	}
	c.responseOnce.Do(func() {
		if c.compact.Cache == CacheHit || c.compact.Cache == CacheStale {
			c.proxy.touchManifestAccess(ctx, meta.ManifestRecord{
				Digest: c.compact.Digest, MediaType: c.compact.MediaType, Size: c.compact.Size,
				ObjectKey: c.compact.Digest,
			})
		}
		c.proxy.recordManifestPull(ctx, c.req, &c.compact)
		c.proxy.publishCacheAccess(ctx, c.req, &c.compact)
	})
	return nil
}

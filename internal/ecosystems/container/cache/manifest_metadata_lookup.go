package cache

import (
	"context"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func (p manifestProxy) lookupMetadata(ctx context.Context, req ManifestRequest) (*meta.ManifestRecord, bool, error) {
	now := time.Now()
	if reference.IsDigest(req.Reference) {
		digest, err := digestFromReference(req.Reference, nil)
		if err != nil {
			return nil, false, err
		}
		return p.lookupManifestRecord(ctx, meta.ManifestKey{
			Alias:      req.UpstreamAlias,
			Repository: req.Repo,
			Digest:     digest,
		}, now)
	}

	tag, ok, err := p.metadata.Tag(ctx, meta.TagKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Reference:  req.Reference,
	})
	if err != nil {
		return nil, false, wrapError(err, "lookup manifest tag")
	}
	if !ok || tagExpired(tag, now) {
		return nil, false, nil
	}
	return p.lookupManifestRecord(ctx, meta.ManifestKey{
		Alias:      req.UpstreamAlias,
		Repository: req.Repo,
		Digest:     tag.Digest,
	}, now)
}

func tagExpired(tag *meta.TagRecord, now time.Time) bool {
	return !tag.ExpiresAt.IsZero() && !now.Before(tag.ExpiresAt)
}

func (p manifestProxy) lookupManifestRecord(ctx context.Context, key meta.ManifestKey, now time.Time) (*meta.ManifestRecord, bool, error) {
	record, ok, err := p.metadata.Manifest(ctx, key)
	if err != nil {
		return nil, false, wrapError(err, "lookup manifest record")
	}
	if !ok || record.Expired(now) {
		return nil, false, nil
	}
	return record, true, nil
}

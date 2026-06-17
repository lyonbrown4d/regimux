package cache

import (
	"context"
	"io"
)

func closeHTTPBodyWithError(body io.Closer, err error, label string) error {
	closeErr := closeHTTPBody(body, label)
	if closeErr != nil {
		return joinError("close response after error", err, closeErr)
	}
	return err
}

func (p manifestProxy) deleteInvalidManifestCache(ctx context.Context, cacheKey string) (*CachedManifest, bool, error) {
	if deleteErr := p.cache.Delete(ctx, cacheKey); deleteErr != nil {
		return nil, false, wrapError(deleteErr, "delete invalid manifest cache entry")
	}
	return nil, false, nil
}

func (p referrerProxy) deleteInvalidReferrersCache(ctx context.Context, cacheKey string) (*ReferrersResult, bool, error) {
	if deleteErr := p.cache.Delete(ctx, cacheKey); deleteErr != nil {
		return nil, false, wrapError(deleteErr, "delete invalid referrers cache entry")
	}
	return nil, false, nil
}

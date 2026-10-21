package cache

import (
	"context"
	"errors"
	"io"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const maxProxyMetadataBodyBytes int64 = 16 << 20

func readHTTPBody(body io.ReadCloser, label string) ([]byte, error) {
	if body == nil {
		return nil, nil
	}

	data, readErr := upstreamhttp.ReadAllLimited(body, maxProxyMetadataBodyBytes)
	closeErr := body.Close()
	if readErr != nil {
		return nil, wrapError(readErr, "read %s", label)
	}
	if closeErr != nil {
		return nil, wrapError(closeErr, "close %s", label)
	}
	if err := jsonbudget.Check(data, jsonbudget.DefaultLimits()); errors.Is(err, jsonbudget.ErrLimit) {
		return nil, wrapError(err, "check %s JSON budget", label)
	}
	return data, nil
}

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

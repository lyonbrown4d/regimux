package api_test

import (
	"context"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type recordingManifestService struct{}

func (s *recordingManifestService) Get(_ context.Context, _ cache.ManifestRequest) (*cache.CachedManifest, error) {
	return &cache.CachedManifest{
		Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MediaType: distribution.MediaTypeOCIManifest,
		Size:      2,
		Body:      []byte("{}"),
		Headers:   http.Header{distribution.HeaderContentLength: {"2"}},
		Cache:     cache.CacheBypass,
	}, nil
}

var _ cache.ManifestService = (*recordingManifestService)(nil)

package prefetch_test

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
)

type fakeManifestService struct {
	mu        sync.Mutex
	manifests map[string]*cache.CachedManifest
	entries   []cache.ManifestRequest
}

func newFakeManifestService(manifests map[string]*cache.CachedManifest) *fakeManifestService {
	return &fakeManifestService{manifests: manifests}
}

func (f *fakeManifestService) Get(_ context.Context, req cache.ManifestRequest) (*cache.CachedManifest, error) {
	return f.get(req)
}

func (f *fakeManifestService) Refresh(_ context.Context, req cache.ManifestRequest) (*cache.CachedManifest, error) {
	return f.get(req)
}

func (f *fakeManifestService) get(req cache.ManifestRequest) (*cache.CachedManifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, req)
	manifest, ok := f.manifests[req.Reference]
	if !ok {
		return nil, errors.New("manifest not found")
	}
	return manifest, nil
}

func (f *fakeManifestService) requestSnapshot() []cache.ManifestRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.entries)
}

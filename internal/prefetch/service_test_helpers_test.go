package prefetch_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/prefetch"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func runPrefetch(
	ctx context.Context,
	t *testing.T,
	store meta.Store,
	manifests cache.ManifestService,
	blobs cache.BlobService,
) (*prefetch.RunReport, error) {
	t.Helper()
	service := prefetch.NewService(prefetch.ServiceDependencies{
		Metadata:  store,
		Tags:      fakeTagService{tags: []string{sourceTag, targetTag}},
		Manifests: manifests,
		Blobs:     blobs,
		Logger:    slog.New(slog.DiscardHandler),
	})
	report, err := service.Run(ctx, prefetch.RunOptions{
		MaxRecords:           10,
		MinPullCount:         1,
		TagsPageSize:         100,
		MaxCandidatesPerRepo: 1,
		MaxVersionDistance:   10,
		Now:                  time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return nil, fmt.Errorf("run prefetch service: %w", err)
	}
	return report, nil
}

func newPrefetchMetaStore(ctx context.Context, t *testing.T) meta.Store {
	t.Helper()
	store, err := meta.OpenBboltWithOptions(ctx, meta.BboltOptions{
		Path: filepath.Join(t.TempDir(), "meta.db"),
	})
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close metadata store: %v", err)
		}
	})
	return store
}

func recordObservedPull(ctx context.Context, t *testing.T, store meta.Store) {
	t.Helper()
	_, err := store.RecordPull(ctx, meta.PullKey{
		Alias:      testAlias,
		Repository: testRepo,
		Reference:  sourceTag,
	}, time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("record pull: %v", err)
	}
}

func imageManifestBody(t *testing.T, configDigest string, layerDigests ...string) []byte {
	t.Helper()
	layers := make([]ocispec.Descriptor, 0, len(layerDigests))
	for i := range layerDigests {
		layers = append(layers, ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageLayerGzip,
			Digest:    digest.Digest(layerDigests[i]),
			Size:      100 + int64(i),
		})
	}
	return marshalManifest(t, ocispec.Manifest{
		MediaType: distribution.MediaTypeOCIManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    digest.Digest(configDigest),
			Size:      64,
		},
		Layers: layers,
	})
}

func indexManifestBody(t *testing.T, childDigest string) []byte {
	t.Helper()
	return marshalManifest(t, ocispec.Index{
		MediaType: distribution.MediaTypeOCIIndex,
		Manifests: []ocispec.Descriptor{{
			MediaType: distribution.MediaTypeOCIManifest,
			Digest:    digest.Digest(childDigest),
			Size:      512,
		}},
	})
}

func marshalManifest(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return body
}

func cachedManifest(manifestDigest, mediaType string, body []byte) *cache.CachedManifest {
	return &cache.CachedManifest{
		Digest:    manifestDigest,
		MediaType: mediaType,
		Size:      int64(len(body)),
		Body:      body,
		Headers:   http.Header{distribution.HeaderContentType: {mediaType}},
		Cache:     cache.CacheMiss,
	}
}

func assertReport(t *testing.T, report *prefetch.RunReport, prefetched, failed int) {
	t.Helper()
	if report.Candidates != 1 || report.Prefetched != prefetched || report.Failed != failed {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func assertManifestReferences(t *testing.T, requests []cache.ManifestRequest, want []string) {
	t.Helper()
	if len(requests) != len(want) {
		t.Fatalf("manifest requests = %d, want %d: %#v", len(requests), len(want), requests)
	}
	for i := range want {
		if requests[i].Reference != want[i] || requests[i].Method != http.MethodGet {
			t.Fatalf("unexpected manifest request %d: %#v", i, requests[i])
		}
	}
}

func assertBlobRequests(t *testing.T, requests []cache.BlobRequest, want []string) {
	t.Helper()
	if len(requests) != len(want) {
		t.Fatalf("blob requests = %d, want %d: %#v", len(requests), len(want), requests)
	}
	for i := range want {
		if !slices.ContainsFunc(requests, func(req cache.BlobRequest) bool {
			return req.Digest == want[i] && req.Method == http.MethodGet
		}) {
			t.Fatalf("missing blob GET for digest %s in %#v", want[i], requests)
		}
	}
}

func assertClosedBlobReaders(t *testing.T, closed, want []string) {
	t.Helper()
	if len(closed) != len(want) {
		t.Fatalf("closed blob readers = %d, want %d: %#v", len(closed), len(want), closed)
	}
	for i := range want {
		if !slices.Contains(closed, want[i]) {
			t.Fatalf("missing closed reader for digest %s in %#v", want[i], closed)
		}
	}
}

func testDigest(char string) string {
	return "sha256:" + strings.Repeat(char, 64)
}

type fakeTagService struct {
	tags []string
}

func (f fakeTagService) List(context.Context, cache.TagRequest) (*cache.TagsResult, error) {
	body, err := json.Marshal(struct {
		Tags []string `json:"tags"`
	}{Tags: f.tags})
	if err != nil {
		return nil, fmt.Errorf("marshal fake tags: %w", err)
	}
	return &cache.TagsResult{Body: body}, nil
}

type fakeManifestService struct {
	mu        sync.Mutex
	manifests map[string]*cache.CachedManifest
	entries   []cache.ManifestRequest
}

func newFakeManifestService(manifests map[string]*cache.CachedManifest) *fakeManifestService {
	return &fakeManifestService{manifests: manifests}
}

func (f *fakeManifestService) Get(_ context.Context, req cache.ManifestRequest) (*cache.CachedManifest, error) {
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
	return append([]cache.ManifestRequest(nil), f.entries...)
}

type fakeBlobService struct {
	mu       sync.Mutex
	entries  []cache.BlobRequest
	closures []string
}

func (f *fakeBlobService) Get(_ context.Context, req cache.BlobRequest) (*cache.BlobReadResult, error) {
	f.mu.Lock()
	f.entries = append(f.entries, req)
	f.mu.Unlock()
	return &cache.BlobReadResult{
		Reader: &trackedReader{onClose: func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.closures = append(f.closures, req.Digest)
		}},
		Digest: req.Digest,
		Cache:  cache.CacheMiss,
	}, nil
}

func (f *fakeBlobService) requestSnapshot() []cache.BlobRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cache.BlobRequest(nil), f.entries...)
}

func (f *fakeBlobService) closedSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closures...)
}

type trackedReader struct {
	onClose func()
}

func (r *trackedReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (r *trackedReader) Close() error {
	if r.onClose != nil {
		r.onClose()
	}
	return nil
}

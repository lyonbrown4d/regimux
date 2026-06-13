package container_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestRegistryEndpointManifestGetFillsImageManifestBlobsAsync(t *testing.T) {
	configDigest := endpointTestDigest("c")
	layerDigest := endpointTestDigest("1")
	secondLayerDigest := endpointTestDigest("2")
	manifests := endpointManifestService{
		manifest: cachedEndpointManifest(
			endpointTestDigest("m"),
			distribution.MediaTypeOCIManifest,
			endpointImageManifestBody(t, configDigest, layerDigest, secondLayerDigest),
		),
	}
	blobs := newEndpointBlobService()
	endpoint := container.NewRegistryEndpoint(manifests, blobs, nil, nil, slog.New(slog.DiscardHandler))
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}

	blobs.release()
	blobs.waitRequests(t, 3)
	blobs.waitClosed(t, 3)
	assertEndpointBlobRequests(t, blobs.requestSnapshot(), []string{configDigest, layerDigest, secondLayerDigest})
	assertEndpointBlobReadersClosed(t, blobs.closedSnapshot(), []string{configDigest, layerDigest, secondLayerDigest})
}

func TestRegistryEndpointHeadManifestDoesNotFillBlobs(t *testing.T) {
	manifests := endpointManifestService{
		manifest: cachedEndpointManifest(
			endpointTestDigest("m"),
			distribution.MediaTypeOCIManifest,
			endpointImageManifestBody(t, endpointTestDigest("c"), endpointTestDigest("1")),
		),
	}
	blobs := newEndpointBlobService()
	endpoint := container.NewRegistryEndpoint(manifests, blobs, nil, nil, slog.New(slog.DiscardHandler))
	baseURL := startAPIServer(t, endpoint)

	resp := httpHead(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	blobs.assertNoRequests(t)
}

func TestRegistryEndpointIndexManifestDoesNotFillBlobs(t *testing.T) {
	manifests := endpointManifestService{
		manifest: cachedEndpointManifest(
			endpointTestDigest("m"),
			distribution.MediaTypeOCIIndex,
			endpointIndexManifestBody(t, endpointTestDigest("child")),
		),
	}
	blobs := newEndpointBlobService()
	endpoint := container.NewRegistryEndpoint(manifests, blobs, nil, nil, slog.New(slog.DiscardHandler))
	baseURL := startAPIServer(t, endpoint)

	resp := httpGet(t, baseURL+"/v2/hub/library/alpine/manifests/latest")
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", resp.StatusCode, body)
	}
	blobs.assertNoRequests(t)
}

func endpointImageManifestBody(t *testing.T, configDigest string, layerDigests ...string) []byte {
	t.Helper()
	layers := make([]ocispec.Descriptor, 0, len(layerDigests))
	for i := range layerDigests {
		layers = append(layers, ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageLayerGzip,
			Digest:    digest.Digest(layerDigests[i]),
			Size:      100 + int64(i),
		})
	}
	return marshalEndpointManifest(t, ocispec.Manifest{
		MediaType: distribution.MediaTypeOCIManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    digest.Digest(configDigest),
			Size:      64,
		},
		Layers: layers,
	})
}

func endpointIndexManifestBody(t *testing.T, childDigest string) []byte {
	t.Helper()
	return marshalEndpointManifest(t, ocispec.Index{
		MediaType: distribution.MediaTypeOCIIndex,
		Manifests: []ocispec.Descriptor{{
			MediaType: distribution.MediaTypeOCIManifest,
			Digest:    digest.Digest(childDigest),
			Size:      512,
		}},
	})
}

func marshalEndpointManifest(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return body
}

func cachedEndpointManifest(manifestDigest, mediaType string, body []byte) *cache.CachedManifest {
	return &cache.CachedManifest{
		Digest:    manifestDigest,
		MediaType: mediaType,
		Size:      int64(len(body)),
		Body:      body,
		Headers:   http.Header{distribution.HeaderContentLength: {strconv.Itoa(len(body))}},
		Cache:     cache.CacheMiss,
	}
}

func endpointTestDigest(char string) string {
	return "sha256:" + strings.Repeat(char, 64)
}

func assertEndpointBlobRequests(t *testing.T, requests []cache.BlobRequest, want []string) {
	t.Helper()
	if len(requests) != len(want) {
		t.Fatalf("blob requests = %d, want %d: %#v", len(requests), len(want), requests)
	}
	for i := range want {
		if !slices.ContainsFunc(requests, func(req cache.BlobRequest) bool {
			return req.UpstreamAlias == "hub" &&
				req.Repo == "library/alpine" &&
				req.Digest == want[i] &&
				req.Method == http.MethodGet &&
				req.Range == nil
		}) {
			t.Fatalf("missing blob fill GET for digest %s in %#v", want[i], requests)
		}
	}
}

func assertEndpointBlobReadersClosed(t *testing.T, closed, want []string) {
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

type endpointManifestService struct {
	manifest *cache.CachedManifest
}

func (s endpointManifestService) Get(context.Context, cache.ManifestRequest) (*cache.CachedManifest, error) {
	return s.manifest, nil
}

type endpointBlobService struct {
	mu       sync.Mutex
	requests []cache.BlobRequest
	closed   []string
	released chan struct{}
	once     sync.Once
}

func newEndpointBlobService() *endpointBlobService {
	return &endpointBlobService{released: make(chan struct{})}
}

func (s *endpointBlobService) Get(ctx context.Context, req cache.BlobRequest) (*cache.BlobReadResult, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	select {
	case <-s.released:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for blob release: %w", ctx.Err())
	}
	return &cache.BlobReadResult{
		Reader: &endpointBlobReader{
			onClose: func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.closed = append(s.closed, req.Digest)
			},
		},
		Digest: req.Digest,
		Cache:  cache.CacheMiss,
	}, nil
}

func (s *endpointBlobService) release() {
	s.once.Do(func() {
		close(s.released)
	})
}

func (s *endpointBlobService) requestSnapshot() []cache.BlobRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]cache.BlobRequest(nil), s.requests...)
}

func (s *endpointBlobService) closedSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.closed...)
}

func (s *endpointBlobService) waitRequests(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.requestSnapshot()) >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("blob requests = %d, want %d: %#v", len(s.requestSnapshot()), count, s.requestSnapshot())
}

func (s *endpointBlobService) waitClosed(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.closedSnapshot()) >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("closed blob readers = %d, want %d: %#v", len(s.closedSnapshot()), count, s.closedSnapshot())
}

func (s *endpointBlobService) assertNoRequests(t *testing.T) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	if requests := s.requestSnapshot(); len(requests) != 0 {
		t.Fatalf("blob requests = %#v, want none", requests)
	}
}

type endpointBlobReader struct {
	onClose func()
}

func (r *endpointBlobReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (r *endpointBlobReader) Close() error {
	if r.onClose != nil {
		r.onClose()
	}
	return nil
}

var (
	_ cache.ManifestService = endpointManifestService{}
	_ cache.BlobService     = (*endpointBlobService)(nil)
)

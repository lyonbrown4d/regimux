package dist

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
)

func TestDistWarmMetadataReadsCoalesceWithIndependentRanges(t *testing.T) {
	const readers = 128
	body := []byte("dist warm independent range body")
	metadata, objects := newDistReadFixture(t, body)
	service := NewService(ServiceDependencies{Metadata: metadata, Objects: objects})
	var joined sync.WaitGroup
	joined.Add(readers)
	results := make(chan distReadResult, readers)
	for index := range readers {
		go func() {
			request := Request{Method: http.MethodGet}
			if index%2 != 0 {
				request.Range = "bytes=2-6"
			}
			ctx := &distReadWaitContext{Context: t.Context(), ready: &joined}
			response, found, err := service.cached(ctx, request, Route{Alias: "nodejs", Repository: "v1", Reference: "node.tar.xz"})
			results <- distReadResult{response: response, found: found, err: err, ranged: index%2 != 0}
		}()
	}
	ready := make(chan struct{})
	go func() { joined.Wait(); close(ready) }()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		close(metadata.release)
		t.Fatal("dist readers did not enter the blocked cache read")
	}
	close(metadata.release)
	for range readers {
		assertDistWarmRead(t, <-results, body)
	}
	if metadata.tags.Load() != 1 || metadata.manifests.Load() != 1 || metadata.blobs.Load() != 1 || metadata.touches.Load() != 1 {
		t.Fatalf("dist metadata tag/manifest/blob/touch = %d/%d/%d/%d, want 1 each", metadata.tags.Load(), metadata.manifests.Load(), metadata.blobs.Load(), metadata.touches.Load())
	}
}

func TestDistSharedMetadataPreservesTagAndManifestExpiry(t *testing.T) {
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	for _, source := range []string{"tag", "manifest", "fresh"} {
		t.Run(source, func(t *testing.T) {
			assertDistSharedMetadataExpiry(t, source, now)
		})
	}
}

func assertDistSharedMetadataExpiry(t *testing.T, source string, now time.Time) {
	t.Helper()
	metadata, objects := newDistReadFixture(t, []byte("dist expiry body"))
	close(metadata.release)
	if source == "tag" {
		metadata.tag.ExpiresAt = now.Add(-time.Second)
	}
	if source == "manifest" {
		metadata.manifest.ExpiresAt = now.Add(-time.Second)
	}
	service := NewService(ServiceDependencies{Metadata: metadata, Objects: objects, Now: func() time.Time { return now }})
	response, found, err := service.cached(t.Context(), Request{Method: http.MethodGet}, Route{})
	if err != nil || !found {
		t.Fatalf("dist expiry cache read found/error = %v/%v", found, err)
	}
	if err := response.body.Close(); err != nil {
		t.Fatalf("close dist expiry body: %v", err)
	}
	if response.expired != (source != "fresh") {
		t.Fatalf("dist cached expiry = %v for %s", response.expired, source)
	}
}

func newDistReadFixture(t *testing.T, body []byte) (*distReadMetadata, object.Store) {
	t.Helper()
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("create dist objects: %v", err)
	}
	digest := ocidigest.SHA256.FromBytes(body).String()
	if _, err := objects.Put(t.Context(), digest, bytes.NewReader(body), object.PutOptions{}); err != nil {
		t.Fatalf("seed dist object: %v", err)
	}
	return &distReadMetadata{
		tag: meta.TagRecord{Digest: digest},
		manifest: meta.ManifestRecord{
			Digest: digest, ObjectKey: digest, Size: int64(len(body)),
			Headers: map[string][]string{"Etag": {"dist-original"}},
		},
		release: make(chan struct{}),
	}, objects
}

func assertDistWarmRead(t *testing.T, result distReadResult, body []byte) {
	t.Helper()
	if result.err != nil || !result.found {
		t.Fatalf("dist warm read found/error = %v/%v", result.found, result.err)
	}
	response := result.response
	want, status := body, http.StatusOK
	if result.ranged {
		want, status = body[2:7], http.StatusPartialContent
	}
	got, readErr := io.ReadAll(response.body)
	closeErr := response.body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, want) || response.status != status {
		t.Fatalf("dist warm body/status = %q/%d, errors %v/%v", got, response.status, readErr, closeErr)
	}
	if response.headers.Get("ETag") != "dist-original" {
		t.Fatal("dist response headers were shared")
	}
	response.headers.Set("ETag", "caller-only")
}

type distReadResult struct {
	response storedResponse
	err      error
	found    bool
	ranged   bool
}

type distReadWaitContext struct {
	context.Context
	ready *sync.WaitGroup
	once  sync.Once
}

func (c *distReadWaitContext) Done() <-chan struct{} {
	c.once.Do(c.ready.Done)
	return c.Context.Done()
}

type distReadMetadata struct {
	meta.Store
	tag       meta.TagRecord
	manifest  meta.ManifestRecord
	release   chan struct{}
	tags      atomic.Int64
	manifests atomic.Int64
	blobs     atomic.Int64
	touches   atomic.Int64
}

func (m *distReadMetadata) Tag(ctx context.Context, _ meta.TagKey) (*meta.TagRecord, bool, error) {
	m.tags.Add(1)
	select {
	case <-m.release:
		return &m.tag, true, nil
	case <-ctx.Done():
		return nil, false, fmt.Errorf("dist metadata gate: %w", ctx.Err())
	}
}

func (m *distReadMetadata) Manifest(context.Context, meta.ManifestKey) (*meta.ManifestRecord, bool, error) {
	m.manifests.Add(1)
	return &m.manifest, true, nil
}

func (m *distReadMetadata) Blob(context.Context, meta.BlobKey) (*meta.BlobRecord, bool, error) {
	m.blobs.Add(1)
	return &meta.BlobRecord{Digest: m.manifest.Digest}, true, nil
}

func (m *distReadMetadata) TouchBlob(context.Context, meta.BlobKey, time.Time) error {
	m.touches.Add(1)
	return nil
}

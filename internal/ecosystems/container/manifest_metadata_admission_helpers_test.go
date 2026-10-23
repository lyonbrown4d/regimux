package container_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/cache/backend"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/reference"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/upstream"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func startAdmissionRequest(results chan<- admissionManifestResponse, baseURL, tag string) {
	go func() {
		results <- fetchAdmissionManifest(baseURL + "/v2/hub/library/alpine-" + tag + "/manifests/latest")
	}()
}

func waitForAdmissionManifestCalls(client *admissionManifestClient, want int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if client.calls.Load() >= want {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func waitForColdMetadataWrite(entered <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-entered:
		return true
	case <-time.After(timeout):
		return false
	}
}

func waitForAdmissionPull(entered <-chan meta.PullKey, timeout time.Duration) bool {
	select {
	case <-entered:
		return true
	case <-time.After(timeout):
		return false
	}
}

func assertAdmissionResponses(t *testing.T, results <-chan admissionManifestResponse, requests int, body []byte) {
	t.Helper()
	for range requests {
		select {
		case response := <-results:
			if response.err != nil || response.status != http.StatusOK || !bytes.Equal(response.body, body) {
				t.Errorf("manifest response status=%d body=%q error=%v", response.status, response.body, response.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("manifest request did not finish after releasing pull recording")
		}
	}
}

func assertAdmissionPullCounts(t *testing.T, metadata meta.Store, requests int) {
	t.Helper()
	for i := range requests {
		key := meta.PullKey{Alias: "hub", Repository: "library/alpine", Reference: strconv.Itoa(i)}
		pull, found, err := metadata.Pull(t.Context(), key)
		if err != nil || !found {
			t.Fatalf("read persisted pull %s: found=%t error=%v", key.String(), found, err)
		}
		if pull.Count != 1 {
			t.Errorf("persisted pull %s count=%d, want 1", key.String(), pull.Count)
		}
	}
}

func seedAdmissionManifest(t *testing.T, store backend.Backend, tag string, body []byte) {
	t.Helper()
	envelope := struct {
		Record meta.ManifestRecord `json:"record"`
		Body   []byte              `json:"body"`
	}{
		Record: meta.ManifestRecord{
			Digest: endpointTestDigest("a"), MediaType: distribution.MediaTypeOCIManifest, Size: int64(len(body)),
		},
		Body: body,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	key := "manifest:hub:library/alpine:" + tag + ":" + reference.AcceptKey("")
	if err := store.Set(t.Context(), key, data, time.Hour); err != nil {
		t.Fatalf("seed manifest %s: %v", tag, err)
	}
}

type blockedManifestPullStore struct {
	meta.Store
	entered chan meta.PullKey
	release chan struct{}
	once    sync.Once
}

type blockedManifestMetadataStore struct {
	meta.Store
	operation string
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (s *blockedManifestMetadataStore) wait(ctx context.Context, operation string) error {
	if s.operation != operation {
		return nil
	}
	s.entered <- struct{}{}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for %s: %w", operation, ctx.Err())
	}
}

func (s *blockedManifestMetadataStore) RecordUpstreamPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	if err := s.wait(ctx, "upstream pull"); err != nil {
		return nil, err
	}
	pull, err := s.Store.RecordUpstreamPull(ctx, key, at)
	if err != nil {
		return nil, fmt.Errorf("record blocked upstream pull: %w", err)
	}
	return pull, nil
}

func (s *blockedManifestMetadataStore) UpsertManifest(ctx context.Context, record meta.ManifestRecord) (*meta.ManifestRecord, error) {
	if err := s.wait(ctx, "manifest upsert"); err != nil {
		return nil, err
	}
	manifest, err := s.Store.UpsertManifest(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert blocked manifest: %w", err)
	}
	return manifest, nil
}

func (s *blockedManifestMetadataStore) UpsertTag(ctx context.Context, record meta.TagRecord) (*meta.TagRecord, error) {
	if err := s.wait(ctx, "tag upsert"); err != nil {
		return nil, err
	}
	tag, err := s.Store.UpsertTag(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("upsert blocked manifest tag: %w", err)
	}
	return tag, nil
}

func (s *blockedManifestMetadataStore) unblock() {
	s.once.Do(func() { close(s.release) })
}

type admissionManifestClient struct {
	calls atomic.Int64
	body  []byte
}

func (c *admissionManifestClient) GetManifest(_ context.Context, req upstream.GetManifestRequest) (*upstream.ManifestResponse, error) {
	c.calls.Add(1)
	return &upstream.ManifestResponse{
		Body: io.NopCloser(bytes.NewReader(c.body)), MediaType: distribution.MediaTypeOCIManifest,
		Size: int64(len(c.body)), Headers: http.Header{distribution.HeaderContentType: {distribution.MediaTypeOCIManifest}},
	}, nil
}

func (c *admissionManifestClient) Ping(context.Context, string) error { return nil }

func (c *admissionManifestClient) GetBlob(context.Context, upstream.GetBlobRequest) (*upstream.BlobResponse, error) {
	return nil, errors.New("unexpected blob request")
}

func (c *admissionManifestClient) ConsumeBlob(context.Context, upstream.GetBlobRequest, upstream.BlobConsumeFunc) error {
	return errors.New("unexpected blob consume")
}

func (c *admissionManifestClient) ListTags(context.Context, upstream.ListTagsRequest) (*upstream.TagsResponse, error) {
	return nil, errors.New("unexpected tag request")
}

func (c *admissionManifestClient) GetReferrers(context.Context, upstream.ReferrersRequest) (*upstream.ReferrersResponse, error) {
	return nil, errors.New("unexpected referrers request")
}

var _ upstream.RegistryClient = (*admissionManifestClient)(nil)

func (s *blockedManifestPullStore) RecordPull(ctx context.Context, key meta.PullKey, at time.Time) (*meta.PullRecord, error) {
	s.entered <- key
	select {
	case <-s.release:
		pull, err := s.Store.RecordPull(ctx, key, at)
		if err != nil {
			return nil, fmt.Errorf("record admitted manifest pull: %w", err)
		}
		return pull, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for manifest pull recording: %w", ctx.Err())
	}
}

func (s *blockedManifestPullStore) unblock() {
	s.once.Do(func() { close(s.release) })
}

type admissionManifestResponse struct {
	status int
	body   []byte
	err    error
}

func fetchAdmissionManifest(url string) admissionManifestResponse {
	response, err := httpGetWithClient(&http.Client{Timeout: 10 * time.Second}, url)
	if err != nil {
		return admissionManifestResponse{err: err}
	}
	body, readErr := io.ReadAll(response.Body)
	return admissionManifestResponse{status: response.StatusCode, body: body, err: errors.Join(readErr, response.Body.Close())}
}

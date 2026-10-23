package container_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	registry "github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type blobHeadFallbackCase struct {
	name                string
	headError, getError error
	status              int
}

func TestRegistryBlobIfRangeHeadFailurePreservesGetAuthorization(t *testing.T) {
	cases := []blobHeadFallbackCase{
		{name: "HEAD unauthorized GET allowed", headError: distribution.ErrUnauthorized, status: http.StatusOK},
		{name: "HEAD unsupported GET allowed", headError: distribution.ErrUnsupported, status: http.StatusOK},
		{name: "GET authorization still enforced", headError: distribution.ErrUnauthorized, getError: distribution.ErrUnauthorized, status: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runBlobHeadFallback(t, tc) })
	}
}

func runBlobHeadFallback(t *testing.T, tc blobHeadFallbackCase) {
	t.Helper()
	blobs := &headRejectedBlobService{headError: tc.headError, getError: tc.getError}
	base := startAPIServer(t, registry.NewRegistryEndpoint(nil, blobs, nil, nil, nil))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/v2/hub/library/test/blobs/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-4")
	req.Header.Set("If-Range", `"stable"`)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != tc.status {
		t.Fatalf("status=%d body=%q want=%d", resp.StatusCode, body, tc.status)
	}
	if tc.status == http.StatusOK && string(body) != "0123456789" {
		t.Fatalf("body=%q", body)
	}
	if blobs.gets.Load() != 1 {
		t.Fatalf("GET calls=%d want 1", blobs.gets.Load())
	}
}

type headRejectedBlobService struct {
	headError error
	getError  error
	gets      atomic.Int32
}

func (s *headRejectedBlobService) Get(ctx context.Context, req cache.BlobRequest) (*cache.BlobReadResult, error) {
	if req.Method == http.MethodHead {
		return nil, s.headError
	}
	s.gets.Add(1)
	if s.getError != nil {
		return nil, s.getError
	}
	return (&transferBlobService{}).Get(ctx, req)
}

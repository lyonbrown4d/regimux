package container_test

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	registry "github.com/lyonbrown4d/regimux/internal/ecosystems/container"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type blobTransferCase struct {
	name, method, rangeValue, ifRange, body, contentRange string
	status                                                int
}

func TestRegistryBlobIfRangeAndHeadSemantics(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := startAPIServer(t, registry.NewRegistryEndpoint(nil, &transferBlobService{}, nil, nil, nil))
	cases := []blobTransferCase{
		{"mismatch", http.MethodGet, "bytes=2-4", `"old"`, "0123456789", "", 200},
		{"match", http.MethodGet, "bytes=2-4", `"stable"`, "234", "bytes 2-4/10", 206},
		{"date match", http.MethodGet, "bytes=2-4", "Wed, 21 Oct 2026 00:00:00 GMT", "234", "bytes 2-4/10", 206},
		{"suffix", http.MethodGet, "bytes=-3", "", "789", "bytes 7-9/10", 206},
		{"open ended", http.MethodGet, "bytes=5-", "", "56789", "bytes 5-9/10", 206},
		{"mismatch ignores invalid range", http.MethodGet, "invalid", `"old"`, "0123456789", "", 200},
		{"weak", http.MethodGet, "bytes=2-4", `W/"stable"`, "0123456789", "", 200},
		{"date mismatch", http.MethodGet, "bytes=2-4", "Tue, 20 Oct 2026 00:00:00 GMT", "0123456789", "", 200},
		{"head ignores invalid range", http.MethodHead, "invalid", "", "", "", 200},
		{"unsatisfiable", http.MethodGet, "bytes=90-", "", "", "bytes */10", 416},
	}
	endpoint := base + "/v2/hub/library/test/blobs/" + digest
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertRegistryBlobTransfer(t, endpoint, tc) })
	}
}

func assertRegistryBlobTransfer(t *testing.T, endpoint string, tc blobTransferCase) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), tc.method, endpoint, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", tc.rangeValue)
	req.Header.Set("If-Range", tc.ifRange)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readHTTPResponse(t, resp)
	if resp.StatusCode != tc.status {
		t.Fatalf("status=%d body=%q; want %d", resp.StatusCode, body, tc.status)
	}
	if tc.status != 416 && string(body) != tc.body {
		t.Fatalf("body=%q want=%q", body, tc.body)
	}
	if got := resp.Header.Get("Content-Range"); got != tc.contentRange {
		t.Fatalf("Content-Range=%q want=%q", got, tc.contentRange)
	}
}

type transferBlobService struct{}

func (*transferBlobService) Get(_ context.Context, req cache.BlobRequest) (*cache.BlobReadResult, error) {
	body := "0123456789"
	headers := http.Header{"Etag": {`"stable"`}, "Last-Modified": {"Wed, 21 Oct 2026 00:00:00 GMT"}}
	status := http.StatusOK
	if req.Range != nil && req.Method != http.MethodHead {
		resolved, err := req.Range.Resolve(int64(len(body)))
		if err != nil {
			return nil, distribution.ErrRangeInvalid.WithDetail(err.Error())
		}
		headers.Set("Content-Range", resolved.ContentRange(int64(len(body))))
		body = body[resolved.Start : resolved.End+1]
		status = http.StatusPartialContent
	}
	headers.Set("Content-Length", strconv.Itoa(len(body)))
	size := int64(len(body))
	if req.Method == http.MethodHead {
		body = ""
	}
	return &cache.BlobReadResult{Reader: io.NopCloser(strings.NewReader(body)), Digest: req.Digest, Size: size, Status: status, Headers: headers, Cache: cache.CacheHit}, nil
}

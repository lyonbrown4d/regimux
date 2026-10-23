package cache_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/container/cache"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestBlobTransferRecoversWarmObjectWithinOriginalRange(t *testing.T) {
	body := []byte("0123456789abcdef")
	digest := testDigestFor(body)
	metadata, objects := newTestStores(t)
	interrupted := &interruptedBlobStore{Store: objects}
	proxy := newTestProxy(&fakeRegistryClient{blobBody: body, blobDigest: digest}, metadata, interrupted, nil, config.DefaultConfig())
	req := cache.BlobRequest{UpstreamAlias: "hub", Repo: "library/test", Digest: digest, Method: http.MethodGet}
	warm, err := proxy.Blobs().Get(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	readAndClose(t, warm.Reader)
	interrupted.armed.Store(true)
	req.Range = &object.HTTPRange{Start: 2, End: 7}
	response, err := proxy.Blobs().Get(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readAndClose(t, response.Reader)); got != "234567" {
		t.Fatalf("body=%q", got)
	}
	if interrupted.resumed.Load() != 1 {
		t.Fatalf("resumed=%d want 1", interrupted.resumed.Load())
	}
}

type interruptedBlobStore struct {
	object.Store
	armed   atomic.Bool
	failed  atomic.Bool
	resumed atomic.Int32
}

func (s *interruptedBlobStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	reader, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return reader, info, fmt.Errorf("open interrupted blob object: %w", err)
	}
	if s.armed.Load() && s.failed.CompareAndSwap(false, true) {
		return &truncatedBlobReader{Reader: io.LimitReader(reader, 2), Closer: reader}, info, nil
	}
	if s.armed.Load() && opts.Range != nil && opts.Range.Start == 4 && opts.Range.End == 7 {
		s.resumed.Add(1)
	}
	return reader, info, nil
}

type truncatedBlobReader struct {
	io.Reader
	io.Closer
}

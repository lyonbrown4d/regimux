package cache_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func seedStoredBlob(t *testing.T, metadata meta.Store, objects object.Store, digest string, body []byte) {
	t.Helper()
	ctx := context.Background()
	info, err := objects.Put(ctx, digest, bytes.NewReader(body), object.PutOptions{ContentType: distribution.MediaTypeOctetStream})
	if err != nil {
		t.Fatalf("seed object: %v", err)
	}
	if _, err := metadata.UpsertBlob(ctx, meta.BlobRecord{
		Digest: info.Digest, Size: info.Size, MediaType: info.ContentType, ObjectKey: digest,
	}); err != nil {
		t.Fatalf("seed blob metadata: %v", err)
	}
	if _, err := metadata.UpsertRepoBlob(ctx, meta.RepoBlobRecord{
		Alias: "hub", Repository: "library/alpine", Digest: digest,
	}); err != nil {
		t.Fatalf("seed repository blob metadata: %v", err)
	}
}

func blobRegistryServer(t *testing.T, digest string, body []byte, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v2/library/alpine/blobs/"+digest {
			t.Errorf("registry path=%q", r.URL.Path)
		}
		w.Header().Set(distribution.HeaderDockerContentDigest, digest)
		w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
		if _, err := w.Write(body); err != nil {
			t.Errorf("write registry body: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

type delayedFirstBlobOpen struct {
	object.Store
	delay time.Duration
	gets  atomic.Int32
}

func (s *delayedFirstBlobOpen) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	if s.gets.Add(1) == 1 {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("wait for delayed object open: %w", ctx.Err())
		case <-timer.C:
		}
	}
	reader, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("get object after injected failure: %w", err)
	}
	return reader, info, nil
}

type failFirstBlobOpen struct {
	object.Store
	gets atomic.Int32
}

func (s *failFirstBlobOpen) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	if s.gets.Add(1) == 1 {
		return nil, nil, errors.New("injected object open failure")
	}
	reader, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("get object after blocked failure: %w", err)
	}
	return reader, info, nil
}

type blockedFirstBlobOpen struct {
	object.Store
	started chan struct{}
}

func (s *blockedFirstBlobOpen) Get(ctx context.Context, _ string, _ object.GetOptions) (io.ReadCloser, *object.Info, error) {
	close(s.started)
	<-ctx.Done()
	return nil, nil, fmt.Errorf("wait for blocked object open: %w", ctx.Err())
}

type failBlobStat struct {
	object.Store
	err error
}

func (s *failBlobStat) Stat(context.Context, string) (*object.Info, error) {
	return nil, s.err
}

package dist

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestDistTransferRecoversWarmObjectWithinOriginalRange(t *testing.T) {
	body := []byte("0123456789abcdef")
	metadata, objects := newDistReadFixture(t, body)
	close(metadata.release)
	interrupted := &interruptedDistStore{Store: objects}
	service := NewService(ServiceDependencies{Metadata: metadata, Objects: interrupted})
	response, found, err := service.cached(t.Context(), Request{Method: http.MethodGet, Range: "bytes=2-7"}, Route{})
	if err != nil || !found {
		t.Fatalf("cached found=%v error=%v", found, err)
	}
	data, readErr := io.ReadAll(response.body)
	closeErr := response.body.Close()
	if readErr != nil || closeErr != nil || string(data) != "234567" {
		t.Fatalf("body=%q read=%v close=%v", data, readErr, closeErr)
	}
	if interrupted.resumed.Load() != 1 {
		t.Fatalf("resumed=%d want 1", interrupted.resumed.Load())
	}
}

type interruptedDistStore struct {
	object.Store
	failed  atomic.Bool
	resumed atomic.Int32
}

func (s *interruptedDistStore) Get(ctx context.Context, digest string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	reader, info, err := s.Store.Get(ctx, digest, opts)
	if err != nil {
		return reader, info, fmt.Errorf("get interrupted dist object: %w", err)
	}
	if s.failed.CompareAndSwap(false, true) {
		return &truncatedDistReader{Reader: io.LimitReader(reader, 2), Closer: reader}, info, nil
	}
	if opts.Range != nil && opts.Range.Start == 4 && opts.Range.End == 7 {
		s.resumed.Add(1)
	}
	return reader, info, nil
}

type truncatedDistReader struct {
	io.Reader
	io.Closer
}

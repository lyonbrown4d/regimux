package dist_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServiceCoalesceWaitFallsBackWhenWarmDistObjectOpenFails(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)

	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warm, err := seed.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "warm cache", err)
	closeBody(t, warm.Body)
	upstreamRequests.Store(0)

	metadataWithFirstMiss := &firstTagMissDistStore{Store: metadata}
	failingObjects := &transientDistObjectStore{Store: objects, failEvery: true}
	service := newDistServiceWithObjects(upstream.URL, metadataWithFirstMiss, failingObjects)
	response, err := service.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "get after cache error from coalescer Wait", err)
	if body := readResponse(t, response); body != testZIPBody {
		t.Fatalf("body=%q want complete artifact", body)
	}
	if got := metadataWithFirstMiss.tagCalls.Load(); got != 2 {
		t.Fatalf("metadata tag reads=%d want initial miss plus one coalescer Wait read", got)
	}
	if got := failingObjects.gets.Load(); got != 1 {
		t.Fatalf("cache object opens=%d want one failed Wait read", got)
	}
	if got := upstreamRequests.Load(); got != 1 {
		t.Fatalf("upstream requests=%d want one direct fallback", got)
	}
	if got := failingObjects.puts.Load(); got != 1 {
		t.Fatalf("object writes=%d want one full upstream fill", got)
	}
}

type firstTagMissDistStore struct {
	meta.Store
	tagCalls atomic.Int32
}

func (s *firstTagMissDistStore) Tag(ctx context.Context, key meta.TagKey) (*meta.TagRecord, bool, error) {
	if s.tagCalls.Add(1) == 1 {
		return nil, false, nil
	}
	tag, ok, err := s.Store.Tag(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("read dist cache tag in test store: %w", err)
	}
	return tag, ok, nil
}

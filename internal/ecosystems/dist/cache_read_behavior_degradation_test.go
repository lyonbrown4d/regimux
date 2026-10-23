package dist_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func TestServiceCacheOpenFailurePreservesRangeIfRangeAndHeadSemantics(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(distRangeFallbackHandler(t, &upstreamRequests))
	t.Cleanup(upstream.Close)
	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warmDistArtifact(t, seed)
	upstreamRequests.Store(0)

	failing := &transientDistObjectStore{Store: objects, failEvery: true}
	service := newDistServiceWithObjects(upstream.URL, metadata, failing)
	assertDistRangeFallback(t, service)
	assertDistIfRangeMismatchFallback(t, service)
	assertDistHeadFallback(t, service)
	if got := upstreamRequests.Load(); got != 3 {
		t.Fatalf("upstream requests=%d want range, mismatched If-Range, and HEAD fallback", got)
	}
	if got := failing.puts.Load(); got != 0 {
		t.Fatalf("object writes=%d want 0 for range and HEAD responses", got)
	}
	assertExistingDistTag(t, metadata)
}

func distRangeFallbackHandler(t *testing.T, requests *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set(distribution.HeaderETag, `"stable"`)
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		if r.Method == http.MethodHead {
			w.Header().Set(distribution.HeaderContentLength, "22")
			return
		}
		if r.Header.Get("Range") == "bytes=2-3" && r.Header.Get("If-Range") == `"stable"` {
			w.Header().Set(distribution.HeaderContentRange, "bytes 2-3/22")
			w.Header().Set(distribution.HeaderContentLength, "2")
			w.WriteHeader(http.StatusPartialContent)
			writeBody(t, w, testZIPBody[2:4])
			return
		}
		writeBody(t, w, testZIPBody)
	}
}

func assertDistRangeFallback(t *testing.T, service *dist.Service) {
	t.Helper()
	response, err := service.Get(context.Background(), dist.Request{
		Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet,
		Range: "bytes=2-3", IfRange: `"stable"`,
	})
	requireNoError(t, "range get after cache open failure", err)
	if response.Status != http.StatusPartialContent || response.Headers.Get(distribution.HeaderContentRange) != "bytes 2-3/22" {
		t.Fatalf("range status=%d Content-Range=%q", response.Status, response.Headers.Get(distribution.HeaderContentRange))
	}
	if body := readResponse(t, response); body != testZIPBody[2:4] {
		t.Fatalf("range body=%q", body)
	}
}

func assertDistIfRangeMismatchFallback(t *testing.T, service *dist.Service) {
	t.Helper()
	response, err := service.Get(context.Background(), dist.Request{
		Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet,
		Range: "bytes=2-3", IfRange: `"old"`,
	})
	requireNoError(t, "mismatched If-Range after cache open failure", err)
	if response.Status != http.StatusOK {
		t.Fatalf("mismatched If-Range status=%d want 200", response.Status)
	}
	if body := readResponse(t, response); body != testZIPBody {
		t.Fatalf("mismatched If-Range body=%q want full artifact", body)
	}
}

func assertDistHeadFallback(t *testing.T, service *dist.Service) {
	t.Helper()
	response, err := service.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodHead})
	requireNoError(t, "head after cache open failure", err)
	if response.Status != http.StatusOK || response.Body != http.NoBody || response.Headers.Get(distribution.HeaderContentLength) != "22" {
		t.Fatalf("HEAD status=%d body=%T Content-Length=%q", response.Status, response.Body, response.Headers.Get(distribution.HeaderContentLength))
	}
}

func assertExistingDistTag(t *testing.T, metadata meta.Store) {
	t.Helper()
	_, ok, err := metadata.Tag(context.Background(), meta.TagKey{Alias: "gradle", Repository: "dist", Reference: "gradle-8.7-bin.zip"})
	if err != nil || !ok {
		t.Fatalf("existing cache tag ok=%v error=%v", ok, err)
	}
}

func TestServiceBoundsConcurrentFallbacksAfterWarmDistObjectOpenErrors(t *testing.T) {
	var upstreamRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamRequests.Add(1)
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)
	seed, metadata, objects := newTestService(context.Background(), t, upstream.URL, nil)
	warmDistArtifact(t, seed)
	upstreamRequests.Store(0)

	const clients = 16
	failing := &transientDistObjectStore{Store: objects, failEvery: true}
	service := newDistServiceWithObjects(upstream.URL, metadata, failing)
	responses := runConcurrentDistGets(t, service, clients)
	for _, response := range responses {
		if body := readResponse(t, response); body != testZIPBody {
			t.Fatalf("concurrent body=%q want artifact bytes", body)
		}
	}
	if got := failing.gets.Load(); got != clients {
		t.Fatalf("cache object opens=%d want at most one per request (%d)", got, clients)
	}
	if got := upstreamRequests.Load(); got == 0 || got > clients {
		t.Fatalf("upstream requests=%d want between 1 and %d", got, clients)
	}
}

type distGetResult struct {
	response *dist.Response
	err      error
}

func runConcurrentDistGets(t *testing.T, service *dist.Service, clients int) []*dist.Response {
	t.Helper()
	start := make(chan struct{})
	results := make(chan distGetResult, clients)
	for range clients {
		go func() {
			<-start
			response, err := service.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
			results <- distGetResult{response: response, err: err}
		}()
	}
	close(start)
	responses := make([]*dist.Response, 0, clients)
	for range clients {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("concurrent Get error: %v", result.err)
			}
			responses = append(responses, result.response)
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for concurrent fallback")
		}
	}
	return responses
}

func warmDistArtifact(t *testing.T, service *dist.Service) {
	t.Helper()
	response, err := service.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet})
	requireNoError(t, "warm cache", err)
	closeBody(t, response.Body)
}

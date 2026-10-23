package dist_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
)

func TestDistTransferRecoversInterruptedHTTPBodyBeforeCaching(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			writeInterruptedDistResponse(t, w)
			return
		}
		writeResumedDistResponse(t, w, r)
	}))
	t.Cleanup(server.Close)
	service, _, _ := newTestService(context.Background(), t, server.URL, nil)
	req := dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet}
	response, err := service.Get(context.Background(), req)
	requireNoError(t, "get interrupted archive", err)
	if got := readResponse(t, response); got != testZIPBody {
		t.Fatalf("body=%q", got)
	}
	cached, err := service.Get(context.Background(), req)
	requireNoError(t, "get recovered cache entry", err)
	if got := readResponse(t, cached); got != testZIPBody {
		t.Fatalf("cached body=%q", got)
	}
	if requests.Load() != 2 {
		t.Fatalf("upstream requests=%d want 2", requests.Load())
	}
}

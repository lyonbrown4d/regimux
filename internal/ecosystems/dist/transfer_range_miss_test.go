package dist_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
)

func TestDistTransferSelectsRangeWhenUpstreamIgnoresIt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=2-4" {
			t.Errorf("upstream Range=%q", got)
		}
		w.Header().Set("ETag", `"stable"`)
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)
	service, _, _ := newTestService(context.Background(), t, upstream.URL, nil)
	response, err := service.Get(t.Context(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip", Method: http.MethodGet, Range: "bytes=2-4"})
	requireNoError(t, "get range", err)
	body := readResponse(t, response)
	if response.Status != http.StatusPartialContent || body != testZIPBody[2:5] || response.Headers.Get("Content-Range") != "bytes 2-4/22" {
		t.Fatalf("status=%d body=%q Content-Range=%q", response.Status, body, response.Headers.Get("Content-Range"))
	}
}

package dist_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

const testZIPBody = "PK\x05\x06\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"

func TestServiceRejectsInvalidZIPWithoutCaching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty body"},
		{name: "non zip body", body: "upstream error"},
		{name: "truncated zip body", body: "PK\x03\x04"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testRejectedZIP(t, test.body)
		})
	}
}

func testRejectedZIP(t *testing.T, rejectedBody string) {
	t.Helper()
	ctx := context.Background()
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(distribution.HeaderContentType, "application/zip")
		if requests.Add(1) == 1 {
			writeBody(t, w, rejectedBody)
			return
		}
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)

	service, metadata, _ := newTestService(ctx, t, upstream.URL, []string{"gradle-*.zip"})
	request := dist.Request{
		Alias:  "gradle",
		Tail:   "gradle-8.7-bin.zip",
		Method: http.MethodGet,
	}

	first, err := service.Get(ctx, request)
	if err == nil {
		if first != nil && first.Body != nil {
			closeBody(t, first.Body)
		}
		t.Fatal("expected invalid ZIP error")
	}
	_, cached, lookupErr := metadata.Tag(ctx, meta.TagKey{
		Alias:      "gradle",
		Repository: "dist",
		Reference:  "gradle-8.7-bin.zip",
	})
	requireNoError(t, "lookup rejected ZIP tag", lookupErr)
	if cached {
		t.Fatal("invalid ZIP was cached")
	}

	second, err := service.Get(ctx, request)
	requireNoError(t, "second get", err)
	if body := readResponse(t, second); body != testZIPBody {
		t.Fatalf("second body length = %d, want %d", len(body), len(testZIPBody))
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream requests = %d, want 2", got)
	}
}

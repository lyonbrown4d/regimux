package golang_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
)

type invalidGoProxyCacheBody struct {
	name          string
	tail          string
	body          string
	contentLength string
}

func TestServiceRejectsInvalidGoProxyCacheBodies(t *testing.T) {
	tests := []invalidGoProxyCacheBody{
		{
			name: "empty module file",
			tail: "example.com/demo/@v/v1.0.0.mod",
		},
		{
			name: "invalid module zip",
			tail: "example.com/demo/@v/v1.0.0.zip",
			body: "not-a-zip",
		},
		{
			name:          "truncated declared body",
			tail:          "example.com/demo/@v/v1.0.0.mod",
			body:          "short",
			contentLength: "16",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertServiceRejectsInvalidGoProxyCacheBody(t, test)
		})
	}
}

func assertServiceRejectsInvalidGoProxyCacheBody(
	t *testing.T,
	test invalidGoProxyCacheBody,
) {
	t.Helper()

	var calls atomic.Int32
	upstream := newInvalidGoProxyBodyUpstream(t, test, &calls)
	service := newTestService(context.Background(), t, upstream.URL)
	for attempt := range 2 {
		assertGoProxyRequestRejected(t, service, test.tail, attempt+1)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2", got)
	}
}

func newInvalidGoProxyBodyUpstream(
	t *testing.T,
	test invalidGoProxyCacheBody,
	calls *atomic.Int32,
) *httptest.Server {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if test.contentLength != "" {
			w.Header().Set("Content-Length", test.contentLength)
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(test.body)); err != nil {
			t.Errorf("write upstream response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func assertGoProxyRequestRejected(
	t *testing.T,
	service *golang.Service,
	tail string,
	attempt int,
) {
	t.Helper()

	resp, err := service.Get(context.Background(), golang.Request{
		Alias: "default",
		Tail:  tail,
	})
	if err != nil {
		return
	}
	if resp != nil && resp.Body != nil {
		closeBody(t, resp.Body)
	}
	t.Fatalf("attempt %d unexpectedly accepted invalid response", attempt)
}

func TestServiceAcceptsEmptyGoProxyVersionList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	service := newTestService(context.Background(), t, upstream.URL)
	resp, err := service.Get(context.Background(), golang.Request{
		Alias: "default",
		Tail:  "example.com/demo/@v/list",
	})
	requireNoError(t, "get empty version list", err)
	if body := responseBody(t, resp); body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}

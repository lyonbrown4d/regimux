package dist_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/httpx"
	"github.com/lyonbrown4d/regimux/internal/api"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
)

type distTransferRangeCase struct {
	name, method, rangeValue, ifRange, body, contentRange string
	status                                                int
}

func TestDistTransferIfRangeHTTPAgainstWarmCache(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"stable"`)
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2026 00:00:00 GMT")
		writeBody(t, w, testZIPBody)
	}))
	t.Cleanup(upstream.Close)
	service, _, _ := newTestService(context.Background(), t, upstream.URL, nil)
	warm, err := service.Get(context.Background(), dist.Request{Alias: "gradle", Tail: "gradle-8.7-bin.zip"})
	requireNoError(t, "warm cache", err)
	closeBody(t, warm.Body)
	base := startDistTransferAPI(t, dist.NewEndpoint(service))
	cases := []distTransferRangeCase{
		{"warm head", http.MethodHead, "", "", "", "", 200},
		{"mismatch", http.MethodGet, "bytes=2-4", `"old"`, testZIPBody, "", 200},
		{"match", http.MethodGet, "bytes=2-4", `"stable"`, testZIPBody[2:5], "bytes 2-4/22", 206},
		{"date match", http.MethodGet, "bytes=2-4", "Wed, 21 Oct 2026 00:00:00 GMT", testZIPBody[2:5], "bytes 2-4/22", 206},
		{"suffix", http.MethodGet, "bytes=-3", "", testZIPBody[19:], "bytes 19-21/22", 206},
		{"open ended", http.MethodGet, "bytes=5-", "", testZIPBody[5:], "bytes 5-21/22", 206},
		{"mismatch ignores invalid range", http.MethodGet, "invalid", `"old"`, testZIPBody, "", 200},
		{"weak", http.MethodGet, "bytes=2-4", `W/"stable"`, testZIPBody, "", 200},
		{"head", http.MethodHead, "invalid", "", "", "", 200},
		{"unsatisfiable", http.MethodGet, "bytes=99-", "", "", "bytes */22", 416},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { assertDistTransferRange(t, base, tc) })
	}
}

func assertDistTransferRange(t *testing.T, base string, tc distTransferRangeCase) {
	t.Helper()
	req, reqErr := http.NewRequestWithContext(t.Context(), tc.method, base+"/dist/gradle/gradle-8.7-bin.zip", http.NoBody)
	requireNoError(t, "build range request", reqErr)
	req.Header.Set("Range", tc.rangeValue)
	req.Header.Set("If-Range", tc.ifRange)
	resp, sendErr := http.DefaultClient.Do(req)
	if sendErr != nil {
		t.Fatalf("send range request: %v", sendErr)
	}
	defer func() { requireNoError(t, "close range response", resp.Body.Close()) }()
	body, readErr := io.ReadAll(resp.Body)
	requireNoError(t, "read range response", readErr)
	if got := resp.Header.Get("ETag"); got != `"stable"` {
		t.Fatalf("ETag=%q want=%q", got, `"stable"`)
	}
	if resp.StatusCode != tc.status {
		t.Fatalf("status=%d body=%q want=%d", resp.StatusCode, body, tc.status)
	}
	if tc.status != 416 && string(body) != tc.body {
		t.Fatalf("body=%q want=%q", body, tc.body)
	}
	if got := resp.Header.Get("Content-Range"); got != tc.contentRange {
		t.Fatalf("Content-Range=%q want=%q", got, tc.contentRange)
	}
}

func startDistTransferAPI(t *testing.T, endpoint httpx.Endpoint) string {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	requireNoError(t, "allocate API address", err)
	address := listener.Addr().String()
	requireNoError(t, "release API address", listener.Close())
	server := api.NewServer(api.Options{Listen: address, Endpoints: collectionlist.NewList(endpoint)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	requireNoError(t, "start API", server.Start(ctx))
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		requireNoError(t, "stop API", server.Stop(shutdown))
	})
	return "http://" + address
}

package golang_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
)

func TestHTTPRangeColdAndWarm(t *testing.T) {
	const body = "module example.com/demo\n"
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("ETag", `"go-v1"`)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)
	service := newTestServiceWithUpstreams(context.Background(), t, map[string]config.DependencyUpstreamConfig{
		"default": {Registry: upstream.URL},
	})
	adapter := stdadapter.New(nil)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(golang.NewEndpoint(service))
	proxy := httptest.NewServer(adapter.Router())
	t.Cleanup(proxy.Close)
	assertProxyRange(t, proxy.URL, "bytes=2-7", "", http.StatusPartialContent, body[2:8], "bytes 2-7/"+strconv.Itoa(len(body)))
	assertProxyRange(t, proxy.URL, "bytes=8-", `"go-v1"`, http.StatusPartialContent, body[8:], "bytes 8-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
	assertProxyRange(t, proxy.URL, "bytes=-4", `"go-v1"`, http.StatusPartialContent, body[len(body)-4:], "bytes "+strconv.Itoa(len(body)-4)+"-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want one full-object fetch", upstreamCalls.Load())
	}
	assertProxyRange(t, proxy.URL, "bytes=2-7", `"old"`, http.StatusOK, body, "")
	assertProxyRange(t, proxy.URL, "bytes=999-", "", http.StatusRequestedRangeNotSatisfiable, "", "bytes */"+strconv.Itoa(len(body)))
}

func TestHTTPRecoveryResumesInterruptedBodyBeforeCaching(t *testing.T) {
	const body = "module example.com/demo\n"
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := upstreamCalls.Add(1)
		w.Header().Set("ETag", `"go-v1"`)
		if call == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			writeResponse(t, w, body[:8])
			return
		}
		if got := r.Header.Get("Range"); got != "bytes=8-"+strconv.Itoa(len(body)-1) {
			t.Errorf("continuation Range = %q", got)
		}
		w.Header().Set("Content-Range", "bytes 8-"+strconv.Itoa(len(body)-1)+"/"+strconv.Itoa(len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)-8))
		w.WriteHeader(http.StatusPartialContent)
		writeResponse(t, w, body[8:])
	}))
	t.Cleanup(upstream.Close)
	service := newTestServiceWithUpstreams(context.Background(), t, map[string]config.DependencyUpstreamConfig{
		"default": {Registry: upstream.URL},
	})
	resp, err := service.Get(context.Background(), golang.Request{Alias: "default", Tail: "example.com/demo/@v/v1.0.0.mod"})
	requireNoError(t, "resumed Go artifact", err)
	assertBody(t, resp, body)
	if upstreamCalls.Load() != 2 {
		t.Fatalf("upstream calls = %d, want initial fetch plus one continuation", upstreamCalls.Load())
	}
}

func TestHTTPRecoveryBoundsContinuationHeaders(t *testing.T) {
	const payload = "abcdefgh"
	var calls atomic.Int32
	retryStarted := make(chan struct{})
	var retryStartedOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("ETag", `"go-v1"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			writeResponse(t, w, payload[:3])
			return
		}
		retryStartedOnce.Do(func() { close(retryStarted) })
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	service := newTestServiceWithUpstreams(context.Background(), t, map[string]config.DependencyUpstreamConfig{
		"default": {Registry: upstream.URL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err := service.Get(ctx, golang.Request{Alias: "default", Tail: "example.com/demo/@v/v1.0.0.mod"})
	if err == nil {
		t.Fatal("expected interrupted transfer to fail after continuation header timeout")
	}
	select {
	case <-retryStarted:
	default:
		t.Fatal("continuation request was not started")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("continuation header timeout took %s, want under one second", elapsed)
	}
}

func assertProxyRange(t *testing.T, baseURL, value, ifRange string, status int, body, contentRange string) {
	t.Helper()
	const path = "/go/default/example.com/demo/@v/v1.0.0.mod"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", value)
	if ifRange != "" {
		req.Header.Set("If-Range", ifRange)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(t, resp.Body)
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != status || string(got) != body {
		t.Fatalf("response status/body = %d/%q, want %d/%q", resp.StatusCode, got, status, body)
	}
	if got := resp.Header.Get("Content-Range"); got != contentRange {
		t.Fatalf("Content-Range = %q, want %q", got, contentRange)
	}
	if status == http.StatusPartialContent && !strings.Contains(resp.Header.Get("Accept-Ranges"), "bytes") {
		t.Fatal("partial response omitted Accept-Ranges: bytes")
	}
}

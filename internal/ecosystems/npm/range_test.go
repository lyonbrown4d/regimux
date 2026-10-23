package npm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestHTTPRangeColdAndWarm(t *testing.T) {
	const body = "0123456789"
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("ETag", `"npm-v1"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)
	service := newTestService(context.Background(), t, upstream.URL, 0)
	adapter := stdadapter.New(nil)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(npm.NewEndpoint(service))
	proxy := httptest.NewServer(adapter.Router())
	t.Cleanup(proxy.Close)
	assertNPMProxyRange(t, proxy.URL, "bytes=2-5", "", http.StatusPartialContent, body[2:6], "bytes 2-5/10")
	assertNPMProxyRange(t, proxy.URL, "bytes=6-", `"npm-v1"`, http.StatusPartialContent, body[6:], "bytes 6-9/10")
	assertNPMProxyRange(t, proxy.URL, "bytes=-4", `"npm-v1"`, http.StatusPartialContent, body[len(body)-4:], "bytes 6-9/10")
	if upstreamCalls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want one full-object fetch", upstreamCalls.Load())
	}
	assertNPMProxyRange(t, proxy.URL, "bytes=2-5", `"old"`, http.StatusOK, body, "")
	assertNPMProxyRange(t, proxy.URL, "bytes=99-", "", http.StatusRequestedRangeNotSatisfiable, "", "bytes */10")
}

func TestHTTPRecoveryResumesInterruptedBodyBeforeCaching(t *testing.T) {
	const body = "0123456789"
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := upstreamCalls.Add(1)
		w.Header().Set("ETag", `"npm-v1"`)
		if call == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			writeResponse(t, w, body[:4])
			return
		}
		if got := r.Header.Get("Range"); got != "bytes=4-9" {
			t.Errorf("continuation Range = %q", got)
		}
		w.Header().Set("Content-Range", "bytes 4-9/10")
		w.Header().Set("Content-Length", "6")
		w.WriteHeader(http.StatusPartialContent)
		writeResponse(t, w, body[4:])
	}))
	t.Cleanup(upstream.Close)
	service := newTestService(context.Background(), t, upstream.URL, 0)
	resp, err := service.Get(context.Background(), npm.Request{Alias: "npmjs", Tail: "left-pad/-/left-pad-1.0.0.tgz"})
	requireNoError(t, "resumed npm artifact", err)
	if got := readBody(t, resp); got != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
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
			w.Header().Set("ETag", `"npm-v1"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			writeResponse(t, w, payload[:3])
			return
		}
		retryStartedOnce.Do(func() { close(retryStarted) })
		<-r.Context().Done()
	}))
	t.Cleanup(upstream.Close)
	ctx := context.Background()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() { requireNoError(t, "close metadata", metadata.Close()) })
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open objects", err)
	service := npm.NewService(npm.ServiceDependencies{
		Config: config.Config{NPM: config.DependencyEcosystemConfig{
			"npmjs": {Registry: upstream.URL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond}},
		}},
		Metadata: metadata, Objects: objects,
	})
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err = service.Get(requestCtx, npm.Request{Alias: "npmjs", Tail: "left-pad/-/left-pad-1.0.0.tgz"})
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

func assertNPMProxyRange(t *testing.T, baseURL, value, ifRange string, status int, body, contentRange string) {
	t.Helper()
	const path = "/npm/npmjs/left-pad/-/left-pad-1.0.0.tgz"
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
	defer closeTestBody(t, resp.Body)
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
	if status == http.StatusPartialContent && resp.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length = %q, want %d", resp.Header.Get("Content-Length"), len(body))
	}
}

package pypi_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
)

func TestPackageTransferSelectsColdRangeWhenOriginIgnoresIt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"wheel-v1"`)
		w.Header().Set("Content-Length", "10")
		writeResponse(t, w, "0123456789")
	}))
	t.Cleanup(upstream.Close)
	service, metadata, _ := newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	tail := packageTailFor(t, upstream.URL, "/packages/demo.whl")
	adapter := stdadapter.New(nil)
	server := httpx.New(httpx.WithAdapter(adapter))
	server.RegisterOnly(pypi.NewEndpoint(service))
	runRangeCases(t, adapter.Router(), tail)
	_, found, err := metadata.Tag(t.Context(), meta.TagKey{
		Alias: "pypi", Repository: "pypi/packages", Reference: strings.TrimPrefix(tail, "packages/"),
	})
	requireNoError(t, "lookup partial wheel tag", err)
	if found {
		t.Fatal("a client range published a complete wheel cache entry")
	}
}

func TestPackageTransferRejectsMismatchedInitialPartialRange(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"wheel-v1"`)
		w.Header().Set("Content-Range", "bytes 0-3/16")
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusPartialContent)
		if _, err := io.WriteString(w, "0123"); err != nil {
			t.Errorf("write partial response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)
	service := newTestService(t.Context(), t, upstream.URL, nil)
	_, err := service.Get(t.Context(), pypi.Request{
		Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.whl"),
		Method: http.MethodGet, Range: "bytes=8-11",
	})
	if err == nil {
		t.Fatal("service accepted a partial response for a different range")
	}
}

func TestPackageTransferRejectsUnsolicitedPartialResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-3/16")
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusPartialContent)
		if _, err := io.WriteString(w, "0123"); err != nil {
			t.Errorf("write unsolicited partial response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)
	service := newTestService(t.Context(), t, upstream.URL, nil)
	_, err := service.Get(t.Context(), pypi.Request{
		Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.whl"), Method: http.MethodGet,
	})
	if err == nil {
		t.Fatal("service accepted an unsolicited partial response")
	}
}

const fallbackWheelBody = "0123456789abcdef"

type interruptedRangeCase struct {
	name         string
	etag         string
	ifRange      string
	wantStatus   int
	wantBody     string
	wantRange    string
	wantRequests int32
}

func TestPackageTransferPositionsIgnoredRangeThroughInterruptedBody(t *testing.T) {
	for _, scenario := range []interruptedRangeCase{
		{name: "strong_etag", etag: `"wheel-v1"`, wantStatus: http.StatusPartialContent,
			wantBody: "89ab", wantRange: "bytes 8-11/16", wantRequests: 2},
		{name: "whole_sha_only_partial_cannot_reopen", wantStatus: http.StatusBadGateway, wantRequests: 1},
		{name: "if_range_mismatch_keeps_whole_sha_recovery", ifRange: `"stale"`,
			wantStatus: http.StatusOK, wantBody: fallbackWheelBody, wantRequests: 2},
	} {
		t.Run(scenario.name, func(t *testing.T) { runInterruptedRangeFallback(t, scenario) })
	}
}

func runInterruptedRangeFallback(t *testing.T, scenario interruptedRangeCase) {
	t.Helper()
	fixture := &interruptedRangeOrigin{t: t, scenario: scenario}
	upstream := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(upstream.Close)
	service, metadata, _ := newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	client, proxyURL := startStdProxy(t, service)
	client.Timeout = 3 * time.Second
	tail := packageTailFor(t, upstream.URL, "/packages/demo.whl")
	query := url.Values{
		"__regimux_project": {"demo"},
		"__regimux_sha256":  {fmt.Sprintf("%x", sha256.Sum256([]byte(fallbackWheelBody)))},
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proxyURL+"/pypi/pypi/"+tail+"?"+query.Encode(), http.NoBody)
	requireNoError(t, "create client range", err)
	request.Header.Set("Range", "bytes=8-11")
	request.Header.Set("If-Range", scenario.ifRange)
	response, err := client.Do(request)
	requireNoError(t, "request ignored range through interrupted origin", err)
	assertInterruptedRangeResponse(t, response, scenario)
	if got := fixture.requests.Load(); got != scenario.wantRequests {
		t.Errorf("origin requests=%d want=%d", got, scenario.wantRequests)
	}
	_, found, err := metadata.Tag(t.Context(), meta.TagKey{
		Alias: "pypi", Repository: "pypi/packages", Reference: strings.TrimPrefix(tail, "packages/"),
	})
	requireNoError(t, "lookup range fallback cache tag", err)
	if found {
		t.Fatal("range fallback published a complete package cache entry")
	}
}

func assertInterruptedRangeResponse(t *testing.T, response *http.Response, scenario interruptedRangeCase) {
	t.Helper()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	closeErr := response.Body.Close()
	requireNoError(t, "read client range fallback", err)
	requireNoError(t, "close client range fallback", closeErr)
	if response.StatusCode != scenario.wantStatus {
		t.Fatalf("status=%d want=%d body=%q", response.StatusCode, scenario.wantStatus, body)
	}
	if scenario.wantBody != "" && string(body) != scenario.wantBody {
		t.Errorf("body=%q want=%q", body, scenario.wantBody)
	}
	if got := response.Header.Get("Content-Range"); got != scenario.wantRange {
		t.Errorf("Content-Range=%q want=%q", got, scenario.wantRange)
	}
}

type interruptedRangeOrigin struct {
	t        *testing.T
	scenario interruptedRangeCase
	requests atomic.Int32
}

func (f *interruptedRangeOrigin) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if f.scenario.etag != "" {
		w.Header().Set("ETag", f.scenario.etag)
	}
	if f.requests.Add(1) == 1 {
		if r.Header.Get("Range") != "bytes=8-11" || r.Header.Get("If-Range") != f.scenario.ifRange {
			f.t.Errorf("initial Range=%q If-Range=%q", r.Header.Get("Range"), r.Header.Get("If-Range"))
		}
		f.sendShortFullBody(w)
		return
	}
	if r.Header.Get("Range") != "bytes=5-15" || r.Header.Get("If-Range") != f.scenario.etag {
		f.t.Errorf("continuation Range=%q If-Range=%q", r.Header.Get("Range"), r.Header.Get("If-Range"))
	}
	w.Header().Set("Content-Length", "11")
	w.Header().Set("Content-Range", "bytes 5-15/16")
	w.WriteHeader(http.StatusPartialContent)
	if _, err := io.WriteString(w, fallbackWheelBody[5:]); err != nil {
		f.t.Error(err)
	}
}

func (f *interruptedRangeOrigin) sendShortFullBody(w http.ResponseWriter) {
	w.Header().Set("Content-Length", "16")
	if _, err := io.WriteString(w, fallbackWheelBody[:5]); err != nil {
		f.t.Error(err)
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.Flush(); err != nil {
		f.t.Error(err)
		return
	}
	connection, _, err := controller.Hijack()
	if err != nil {
		f.t.Error(err)
		return
	}
	if err := connection.Close(); err != nil {
		f.t.Error(err)
	}
}

package pypi_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arcgolabs/httpx"
	stdadapter "github.com/arcgolabs/httpx/adapter/std"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestPackageRangeColdAndWarm(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"wheel-v1"`)
		http.ServeContent(w, r, "demo.whl", time.Time{}, strings.NewReader("0123456789"))
	}))
	defer upstream.Close()
	for _, warm := range []bool{false, true} {
		t.Run(map[bool]string{false: "cold", true: "warm"}[warm], func(t *testing.T) {
			service := newTestService(context.Background(), t, upstream.URL, nil)
			tail := packageTailFor(t, upstream.URL, "/packages/demo.whl")
			if warm {
				seedRangeCache(t, service, tail)
			}
			before := calls.Load()
			adapter := stdadapter.New(nil)
			server := httpx.New(httpx.WithAdapter(adapter))
			server.RegisterOnly(pypi.NewEndpoint(service))
			runRangeCases(t, adapter.Router(), tail)
			if warm && calls.Load() != before {
				t.Fatal("warm ranges requested upstream")
			}
		})
	}
}

func seedRangeCache(t *testing.T, service *pypi.Service, tail string) {
	t.Helper()
	resp, err := service.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: tail})
	requireNoError(t, "seed", err)
	if got := readResponse(t, resp); got != "0123456789" {
		t.Fatal(got)
	}
}

type packageRangeCase struct {
	name, value, ifRange, body, contentRange string
	status                                   int
}

func runRangeCases(t *testing.T, handler http.Handler, tail string) {
	t.Helper()
	for _, tc := range []packageRangeCase{
		{"suffix", "bytes=-4", "", "6789", "bytes 6-9/10", http.StatusPartialContent},
		{"bounded", "bytes=2-4", `"wheel-v1"`, "234", "bytes 2-4/10", http.StatusPartialContent},
		{"if_range_mismatch", "bytes=-4", `"other"`, "0123456789", "", http.StatusOK},
		{"unsatisfiable", "bytes=99-", "", "", "bytes */10", http.StatusRequestedRangeNotSatisfiable},
	} {
		t.Run(tc.name, func(t *testing.T) { checkRangeCase(t, handler, tail, tc) })
	}
}

func checkRangeCase(t *testing.T, handler http.Handler, tail string, tc packageRangeCase) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/pypi/pypi/"+tail, http.NoBody)
	req.Header.Set("Range", tc.value)
	req.Header.Set("If-Range", tc.ifRange)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != tc.status || w.Header().Get("Content-Range") != tc.contentRange {
		t.Fatalf("status=%d Content-Range=%q", w.Code, w.Header().Get("Content-Range"))
	}
	if tc.status != http.StatusRequestedRangeNotSatisfiable && w.Body.String() != tc.body {
		t.Fatalf("body=%q", w.Body.String())
	}
	if w.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatal("missing Accept-Ranges")
	}
}

func TestPackageReturnsBeforeUpstreamBodyCompletes(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		requireNoError(t, "flush upstream headers", http.NewResponseController(w).Flush())
		select {
		case <-release:
			writeResponse(t, w, "0123456789")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer close(release)
	service := newTestService(context.Background(), t, upstream.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.whl")})
	requireNoError(t, "stream headers before body", err)
	defer closeBody(t, resp.Body)
	if resp.Status != 200 {
		t.Fatalf("status=%d", resp.Status)
	}
}

type unavailablePackageObjects struct{ object.Store }

func (s unavailablePackageObjects) Put(context.Context, string, io.Reader, object.PutOptions) (*object.Info, error) {
	return nil, errors.New("injected object outage")
}

func TestPackageCacheWriteFailureStillServesBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, "healthy-wheel") }))
	defer upstream.Close()
	_, metadata, objects := newTestServiceWithStores(context.Background(), t, upstream.URL, nil)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: unavailablePackageObjects{objects},
	})
	resp, err := service.Get(context.Background(), pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.whl")})
	requireNoError(t, "healthy upstream despite cache outage", err)
	if got := readResponse(t, resp); got != "healthy-wheel" {
		t.Fatalf("body=%q", got)
	}
}

func TestPackagePartialResponseNeverPublishesFullCache(t *testing.T) {
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.ServeContent(w, r, "demo.bin", time.Time{}, strings.NewReader("0123456789"))
	}))
	defer upstream.Close()
	service := newTestService(context.Background(), t, upstream.URL, nil)
	req := pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.bin"), Range: "bytes=-4"}
	resp, err := service.Get(context.Background(), req)
	requireNoError(t, "range", err)
	if got := readResponse(t, resp); got != "6789" {
		t.Fatal(got)
	}
	req.Range = ""
	resp, err = service.Get(context.Background(), req)
	requireNoError(t, "full", err)
	if got := readResponse(t, resp); got != "0123456789" {
		t.Fatal(got)
	}
	if calls.Load() != 2 {
		t.Fatalf("upstream calls=%d", calls.Load())
	}
}

func TestPackageCanceledWaiterDoesNotCancelOwner(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		requireNoError(t, "flush upstream headers", http.NewResponseController(w).Flush())
		select {
		case <-release:
			writeResponse(t, w, "0123456789")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	service := newTestService(context.Background(), t, upstream.URL, nil)
	req := pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.bin")}
	owner, err := service.Get(context.Background(), req)
	requireNoError(t, "owner", err)
	defer closeBody(t, owner.Body)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = service.Get(ctx, req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error=%v", err)
	}
	close(release)
	if got := readResponse(t, owner); got != "0123456789" {
		t.Fatal(got)
	}
	resp, err := service.Get(context.Background(), req)
	requireNoError(t, "cached after owner", err)
	if got := readResponse(t, resp); got != "0123456789" || resp.Cache != "hit" {
		t.Fatalf("body=%q cache=%s", got, resp.Cache)
	}
}

func TestSimpleIndexSurvivesObjectWriteFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, "<html>healthy-index</html>") }))
	defer upstream.Close()
	_, metadata, objects := newTestServiceWithStores(context.Background(), t, upstream.URL, nil)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: unavailablePackageObjects{objects},
	})
	resp, err := service.Get(context.Background(), pypi.Request{Alias: "pypi", Tail: "simple/demo/"})
	requireNoError(t, "healthy index despite cache outage", err)
	if got := readResponse(t, resp); got != "<html>healthy-index</html>" {
		t.Fatalf("body=%q", got)
	}
}

func TestSimpleIndexFailsOverRetryableStatus(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, "<html>mirror-index</html>") }))
	defer mirror.Close()
	_, metadata, objects := newTestServiceWithStores(context.Background(), t, primary.URL, nil)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: primary.URL, Mirrors: []string{mirror.URL}}}},
		Metadata: metadata, Objects: objects,
	})
	resp, err := service.Get(context.Background(), pypi.Request{Alias: "pypi", Tail: "simple/demo/"})
	requireNoError(t, "index failover", err)
	if got := readResponse(t, resp); resp.Status != 200 || got != "<html>mirror-index</html>" {
		t.Fatalf("status=%d body=%q", resp.Status, got)
	}
}

func TestPackageHashMismatchIsReportedToReader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, "wrong-wheel") }))
	defer upstream.Close()
	service := newTestService(context.Background(), t, upstream.URL, nil)
	resp, err := service.Get(context.Background(), pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.whl"), SHA256: strings.Repeat("0", 64)})
	requireNoError(t, "headers", err)
	defer closeBody(t, resp.Body)
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("hash mismatch was silently accepted")
	}
}

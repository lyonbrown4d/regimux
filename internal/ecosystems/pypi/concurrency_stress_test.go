package pypi_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestPyPI512ConcurrentSimpleIndexMissCoalesces(t *testing.T) {
	const clients = 512
	const body = "<html><body>stress-index</body></html>"
	var requests atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/html")
		writeResponse(t, w, body)
	}))
	t.Cleanup(upstream.Close)
	service := newTestService(t.Context(), t, upstream.URL, nil)

	entered := make(chan struct{}, clients)
	run := testkit.StartConcurrent(clients, func() (*pypi.Response, error) {
		entered <- struct{}{}
		return service.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: "simple/stress/", SkipPullRecord: true})
	}).WithTimeout(45 * time.Second)
	testkit.WaitForSignal(t, started)
	for range clients {
		<-entered
	}
	releaseOnce.Do(func() { close(release) })

	responses := run.RequireOneMiss(t, cacheMiss, cacheHit, func(resp *pypi.Response) string {
		if got := readResponse(t, resp); got != body {
			t.Fatalf("body = %q, want %q", got, body)
		}
		return resp.Cache
	})
	if len(responses) != clients {
		t.Fatalf("responses = %d, want %d", len(responses), clients)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func TestPyPI256PackageWaitersCancelIndependently(t *testing.T) {
	const canceledWaiters = 128
	const activeWaiters = 128
	const body = "stress-wheel"
	service, req, requests, release := newBlockingPackageFixture(t, body)
	owner := startPackageOwner(t, service, req)
	canceledCtx, cancelWaiters := context.WithCancel(t.Context())
	defer cancelWaiters()
	canceledRun, canceledEntered := startCanceledPackageWaiters(service, req, canceledCtx, canceledWaiters)
	activeRun, activeEntered := startActivePackageWaiters(t, service, req, activeWaiters)
	waitForPackageWaiters(t, canceledEntered, activeEntered, canceledWaiters, activeWaiters)
	cancelWaiters()
	assertCanceledWaiters(t, canceledRun)
	release()
	assertPackageWaveBodies(t, owner, activeRun, body)
	if got := requests.Load(); got != 1 {
		t.Fatalf("upstream requests = %d, want 1", got)
	}
}

func newBlockingPackageFixture(t *testing.T, body string) (*pypi.Service, pypi.Request, *atomic.Int32, func()) {
	t.Helper()
	var requests atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", "12")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush package headers: %v", err)
			return
		}
		select {
		case <-release:
			writeResponse(t, w, body)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(closeRelease)
	service := newTestService(t.Context(), t, upstream.URL, nil)
	req := pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/stress.whl"), SkipPullRecord: true}
	return service, req, &requests, closeRelease
}

func startPackageOwner(t *testing.T, service *pypi.Service, req pypi.Request) *pypi.Response {
	t.Helper()
	owner, err := service.Get(t.Context(), req)
	requireNoError(t, "package owner headers", err)
	return owner
}

func startCanceledPackageWaiters(service *pypi.Service, req pypi.Request, ctx context.Context, count int) (*testkit.ConcurrentRun[error], <-chan struct{}) {
	entered := make(chan struct{}, count)
	run := testkit.StartConcurrent(count, func() (error, error) {
		entered <- struct{}{}
		response, getErr := service.Get(ctx, req)
		if response != nil && response.Body != nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				return nil, fmt.Errorf("close canceled waiter response: %w", closeErr)
			}
		}
		if getErr != nil {
			return nil, fmt.Errorf("get canceled waiter response: %w", getErr)
		}
		return nil, nil
	}).WithTimeout(20 * time.Second)
	return run, entered
}

func startActivePackageWaiters(t *testing.T, service *pypi.Service, req pypi.Request, count int) (*testkit.ConcurrentRun[string], <-chan struct{}) {
	t.Helper()
	entered := make(chan struct{}, count)
	run := testkit.StartConcurrent(count, func() (string, error) {
		entered <- struct{}{}
		resp, getErr := service.Get(t.Context(), req)
		if getErr != nil {
			return "", fmt.Errorf("get active waiter response: %w", getErr)
		}
		if resp == nil || resp.Body == nil {
			return "", errors.New("active waiter returned an empty response")
		}
		data, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return "", fmt.Errorf("read active waiter body: %w", readErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close active waiter body: %w", closeErr)
		}
		if resp.Cache != cacheHit {
			return "", errors.New("active waiter did not read the completed cache fill")
		}
		return string(data), nil
	}).WithTimeout(30 * time.Second)
	return run, entered
}

func waitForPackageWaiters(t *testing.T, canceled, active <-chan struct{}, canceledCount, activeCount int) {
	t.Helper()
	for range canceledCount {
		<-canceled
	}
	for range activeCount {
		<-active
	}
}

func assertCanceledWaiters(t *testing.T, run *testkit.ConcurrentRun[error]) {
	t.Helper()
	for _, waiterErr := range run.Wait(t) {
		if !errors.Is(waiterErr, context.Canceled) {
			t.Fatalf("canceled waiter error = %v, want context.Canceled", waiterErr)
		}
	}
}

func assertPackageWaveBodies(t *testing.T, owner *pypi.Response, run *testkit.ConcurrentRun[string], body string) {
	t.Helper()
	if got := readResponse(t, owner); got != body {
		t.Fatalf("owner body = %q, want %q", got, body)
	}
	for _, waiterBody := range run.Wait(t) {
		if waiterBody != body {
			t.Fatalf("active waiter body = %q, want %q", waiterBody, body)
		}
	}
}

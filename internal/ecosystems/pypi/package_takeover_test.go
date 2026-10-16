package pypi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestPyPIWaitersPromoteAfterPackageStreamAbort(t *testing.T) {
	const followers = 128
	const body = "complete promoted wheel body"
	upstream, calls, ownerStarted, ownerCanceled := newAbortedPackageServer(t, len(body), func(_ int, w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, body)
	})
	service := newTestService(t.Context(), t, upstream.URL, nil)
	req := packageRequest(t, upstream.URL, "/packages/takeover.whl")
	owner := startAbortedPackageOwner(t, service, req, ownerStarted)
	defer closeBody(t, owner.Body)

	entered := make(chan struct{}, followers)
	waiters := testkit.StartConcurrent(followers, func() (string, error) {
		entered <- struct{}{}
		return readPackageBody(t.Context(), service, req)
	}).WithTimeout(30 * time.Second)
	for range followers {
		<-entered
	}
	waitForPackageFillWaiters(t, followers)
	if err := owner.Body.Close(); err != nil {
		t.Fatalf("abort original package stream: %v", err)
	}
	assertPackageBodies(t, waiters.Wait(t), body)
	assertOwnerCanceled(t, ownerCanceled)
	assertUpstreamCalls(t, calls.Load(), 2)
}

func TestPyPIUncacheableReplacementDoesNotChainFillOwners(t *testing.T) {
	const followers = 3
	fallbacks := make(chan struct{}, followers)
	replacementStarted := make(chan struct{})
	releaseReplacement := make(chan struct{})
	var releaseReplacementOnce sync.Once
	releaseReplacementBody := func() { releaseReplacementOnce.Do(func() { close(releaseReplacement) }) }
	defer releaseReplacementBody()
	releaseFallback := make(chan struct{})
	var releaseOnce sync.Once
	releaseFallbackBody := func() { releaseOnce.Do(func() { close(releaseFallback) }) }
	defer releaseFallbackBody()
	upstream, calls, ownerStarted, ownerCanceled := newAbortedPackageServer(t, 10,
		newNotFoundReplacementHandler(replacementStarted, releaseReplacement, fallbacks, releaseFallback),
	)
	service := newTestService(t.Context(), t, upstream.URL, nil)
	req := packageRequest(t, upstream.URL, "/packages/missing.whl")
	owner := startAbortedPackageOwner(t, service, req, ownerStarted)
	defer closeBody(t, owner.Body)

	waiters := testkit.StartConcurrent(followers, func() (int, error) {
		return readPackageStatus(t.Context(), service, req)
	}).WithTimeout(10 * time.Second)
	waitForPackageFillWaiters(t, followers)
	if err := owner.Body.Close(); err != nil {
		t.Fatalf("abort original package stream: %v", err)
	}
	testkit.WaitForSignal(t, replacementStarted)
	waitForPackageFillWaiters(t, followers-1)
	releaseReplacementBody()
	waitForFallbackPackageCalls(t, fallbacks, followers-1)
	releaseFallbackBody()
	assertPackageStatuses(t, waiters.Wait(t), http.StatusNotFound)
	assertOwnerCanceled(t, ownerCanceled)
	assertUpstreamCalls(t, calls.Load(), followers+1)
}

func newAbortedPackageServer(t *testing.T, ownerLength int, response func(int, http.ResponseWriter, *http.Request)) (*httptest.Server, *atomic.Int32, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	var calls atomic.Int32
	ownerStarted := make(chan struct{})
	ownerCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call != 1 {
			response(int(call), w, r)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(ownerLength))
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush aborted owner headers: %v", err)
			return
		}
		close(ownerStarted)
		<-r.Context().Done()
		close(ownerCanceled)
	}))
	t.Cleanup(server.Close)
	return server, &calls, ownerStarted, ownerCanceled
}

func newNotFoundReplacementHandler(replacementStarted chan<- struct{}, releaseReplacement <-chan struct{}, fallbacks chan<- struct{}, releaseFallback <-chan struct{}) func(int, http.ResponseWriter, *http.Request) {
	return func(call int, w http.ResponseWriter, r *http.Request) {
		if call == 2 {
			close(replacementStarted)
			waitForRelease(r, releaseReplacement)
		} else if call > 2 {
			fallbacks <- struct{}{}
			waitForRelease(r, releaseFallback)
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func waitForRelease(r *http.Request, release <-chan struct{}) {
	select {
	case <-release:
	case <-r.Context().Done():
	}
}

func readPackageBody(ctx context.Context, service *pypi.Service, req pypi.Request) (string, error) {
	response, err := service.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("get promoted package response: %w", err)
	}
	if response == nil || response.Body == nil {
		return "", errors.New("empty promoted package response")
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return "", fmt.Errorf("read promoted package body: %w", readErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close promoted package body: %w", closeErr)
	}
	return string(data), nil
}

func readPackageStatus(ctx context.Context, service *pypi.Service, req pypi.Request) (int, error) {
	response, err := service.Get(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("get uncacheable package: %w", err)
	}
	if response == nil {
		return 0, errors.New("empty uncacheable package response")
	}
	if response.Body != nil {
		if err := response.Body.Close(); err != nil {
			return 0, fmt.Errorf("close uncacheable package response: %w", err)
		}
	}
	return response.Status, nil
}

func packageRequest(t *testing.T, upstreamURL, packagePath string) pypi.Request {
	t.Helper()
	return pypi.Request{
		Alias: "pypi", Tail: packageTailFor(t, upstreamURL, packagePath), SkipPullRecord: true,
	}
}

func startAbortedPackageOwner(t *testing.T, service *pypi.Service, req pypi.Request, started <-chan struct{}) *pypi.Response {
	t.Helper()
	owner, err := service.Get(t.Context(), req)
	requireNoError(t, "start aborted package owner", err)
	testkit.WaitForSignal(t, started)
	return owner
}

func assertPackageBodies(t *testing.T, bodies []string, want string) {
	t.Helper()
	for index, body := range bodies {
		if body != want {
			t.Fatalf("follower %d body = %q, want complete wheel", index, body)
		}
	}
}

func assertPackageStatuses(t *testing.T, statuses []int, want int) {
	t.Helper()
	for index, status := range statuses {
		if status != want {
			t.Fatalf("follower %d status = %d, want %d", index, status, want)
		}
	}
}

func assertOwnerCanceled(t *testing.T, canceled <-chan struct{}) {
	t.Helper()
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream owner did not observe the aborted body")
	}
}

func assertUpstreamCalls(t *testing.T, got, want int32) {
	t.Helper()
	if got != want {
		t.Fatalf("upstream calls = %d, want %d", got, want)
	}
}

func waitForFallbackPackageCalls(t *testing.T, calls <-chan struct{}, count int) {
	t.Helper()
	started := 0
	for range count {
		select {
		case <-calls:
			started++
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d fallback package calls started", started, count)
		}
	}
}

func waitForPackageFillWaiters(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	lastWaiters := 0
	for time.Now().Before(deadline) {
		var stacks bytes.Buffer
		if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
			t.Fatalf("write goroutine profile: %v", err)
		}
		waiters := countPackageFillWaiters(stacks.String())
		lastWaiters = waiters
		if waiters >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("only %d package waiters joined the active fill; want %d", lastWaiters, want)
}

func countPackageFillWaiters(stacks string) int {
	waiters := 0
	for goroutine := range strings.SplitSeq(stacks, "\n\n") {
		if strings.Contains(goroutine, "internal/coalescer.(*Fill).Wait") &&
			strings.Contains(goroutine, "pypi.(*Service).beginPackageFill") {
			waiters++
		}
	}
	return waiters
}

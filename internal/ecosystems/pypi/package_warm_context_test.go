package pypi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
)

func TestPackageWarmCacheReadHonorsCallerDeadline(t *testing.T) {
	fixture, seedService, upstream := newPackageTransferFixture(t, false)
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
	seedAndVerifyWarmWheel(t, seedService, tail, fixture)

	blocked := &blockedPackageReadStore{Store: fixture.objects, started: make(chan struct{})}
	service := newObservedPackageService(upstream.URL, fixture.metadata, blocked)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	response, err := service.Get(ctx, pypi.Request{Alias: "pypi", Tail: tail, SkipPullRecord: true})
	requireNoError(t, "open warm wheel before caller deadline", err)
	type readResult struct {
		bytes int
		err   error
	}
	readDone := make(chan readResult, 1)
	go func() {
		bytesRead, readErr := response.Body.Read(make([]byte, 32<<10))
		readDone <- readResult{bytes: bytesRead, err: readErr}
	}()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("warm body did not begin a source read")
	}
	select {
	case result := <-readDone:
		if result.bytes != 0 || !errors.Is(result.err, context.DeadlineExceeded) {
			t.Fatalf("body read bytes=%d error=%v, want zero bytes and caller deadline", result.bytes, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("body read did not stop at the caller deadline")
	}
	closeBody(t, response.Body)
}

func TestPackageWarmReaderCancellationDoesNotCancelConcurrentReader(t *testing.T) {
	fixture, seedService, upstream := newPackageTransferFixture(t, false)
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
	seedAndVerifyWarmWheel(t, seedService, tail, fixture)
	service := newObservedPackageService(upstream.URL, fixture.metadata, fixture.objects)

	canceledCtx, cancelReader := context.WithCancel(t.Context())
	canceledResponse, err := service.Get(canceledCtx, pypi.Request{Alias: "pypi", Tail: tail, SkipPullRecord: true})
	requireNoError(t, "open reader that will be canceled", err)
	activeCtx, cancelActive := context.WithTimeout(t.Context(), warmReadCallerBudget)
	defer cancelActive()
	activeResponse, err := service.Get(activeCtx, pypi.Request{Alias: "pypi", Tail: tail, SkipPullRecord: true})
	requireNoError(t, "open independent reader", err)
	cancelReader()
	bytesRead, readErr := canceledResponse.Body.Read(make([]byte, warmReadChunkBytes))
	if bytesRead != 0 || !errors.Is(readErr, context.Canceled) {
		t.Fatalf("canceled reader bytes=%d error=%v, want zero bytes and caller cancellation", bytesRead, readErr)
	}
	closeBody(t, canceledResponse.Body)
	defer closeBody(t, activeResponse.Body)
	hash := sha256.New()
	activeBytes, err := io.Copy(hash, activeResponse.Body)
	requireNoError(t, "read independent warm wheel", err)
	if activeBytes != progressWheelSize || hex.EncodeToString(hash.Sum(nil)) != fixture.digest {
		t.Fatalf("active reader bytes=%d sha256=%s, want %d/%s", activeBytes, hex.EncodeToString(hash.Sum(nil)), progressWheelSize, fixture.digest)
	}
}

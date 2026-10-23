package pypi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
)

type fillTransport func(*http.Request) (*http.Response, error)

func (f fillTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestPackageWaiterDoesNotDuplicateProgressiveOwnerAfterTenSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64
		client := &http.Client{Transport: fillTransport(func(_ *http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Length": {"10"}}, Body: io.NopCloser(strings.NewReader("0123456789"))}, nil
		})}
		service := NewService(ServiceDependencies{Client: client, Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: "https://index.test"}}}})
		req := Request{Alias: "pypi", Tail: "packages/https/index.test/packages/demo.bin", SkipPullRecord: true}
		owner, err := service.Get(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		defer closeReadCloser(owner.Body, nil, "close owner")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			resp, err := service.Get(ctx, req)
			if resp != nil {
				closeReadCloser(resp.Body, nil, "close waiter")
			}
			done <- err
		}()
		time.Sleep(11 * time.Second)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("upstream calls=%d, duplicated an active owner", calls.Load())
		}
		cancel()
		if err := <-done; err == nil {
			t.Fatal("canceled waiter succeeded")
		}
	})
}

func TestIdlePackageOwnerReleasesFillAndSpool(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		headers http.Header
	}{
		{name: "unknown length", headers: http.Header{}},
		{name: "known length with strong ETag", headers: http.Header{"Content-Length": {"7"}, "Etag": {`"immutable"`}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) { assertIdlePackageOwnerReleased(t, testCase.headers) })
		})
	}
}

func assertIdlePackageOwnerReleased(t *testing.T, headers http.Header) {
	t.Helper()
	client := &http.Client{Transport: fillTransport(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: headers.Clone(), Body: io.NopCloser(strings.NewReader("payload"))}, nil
	})}
	service := NewService(ServiceDependencies{Client: client, Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: "https://index.test"}}}})
	req := Request{Alias: "pypi", Tail: "packages/https/index.test/packages/demo.bin", SkipPullRecord: true}
	resp, err := service.Get(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer closeReadCloser(resp.Body, nil, "close idle owner")
	time.Sleep(31 * time.Second)
	synctest.Wait()
	if len(service.cacheSlots) != 0 {
		t.Error("idle owner retained a temporary spool")
	}
	route, err := ParseTail(req.Alias, req.Tail)
	if err != nil {
		t.Fatal(err)
	}
	fill, owner := service.fills.Begin(artifactKey(route))
	if !owner {
		t.Fatal("idle owner retained the fill")
	}
	service.fills.Finish(artifactKey(route), fill, nil)
}

func TestActivePackageReadOutlivesAbandonmentLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service := NewService(ServiceDependencies{})
		source := &leaseBlockedBody{entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		var finished atomic.Int32
		body := service.packageCacheBody(t.Context(), Request{}, Route{}, &upstreamFetch{
			body: source, headers: http.Header{"Content-Length": {"1"}}, transferContext: t.Context(),
		}, func() { finished.Add(1) })
		defer closeReadCloser(body, nil, "close active owner")
		done := make(chan error, 1)
		go func() { _, err := body.Read(make([]byte, 1)); done <- err }()
		<-source.entered
		time.Sleep(31 * time.Second)
		synctest.Wait()
		assertActivePackageReadOwned(t, service, source, &finished)
		close(source.release)
		if err := <-done; !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("active read error=%v, want unexpected EOF", err)
		}
		if err := body.Close(); err != nil {
			t.Fatal(err)
		}
		if len(service.cacheSlots) != 0 || finished.Load() != 1 {
			t.Fatal("closing the finished read did not release its spool and fill exactly once")
		}
	})
}

func assertActivePackageReadOwned(t *testing.T, service *Service, source *leaseBlockedBody, finished *atomic.Int32) {
	t.Helper()
	if len(service.cacheSlots) != 1 || finished.Load() != 0 {
		t.Error("active read lost its spool or fill")
	}
	select {
	case <-source.closed:
		t.Error("abandonment timer closed an active read")
	default:
	}
}

type leaseBlockedBody struct {
	entered chan struct{}
	release chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (b *leaseBlockedBody) Read([]byte) (int, error) {
	close(b.entered)
	select {
	case <-b.release:
		return 0, io.ErrUnexpectedEOF
	case <-b.closed:
		return 0, io.ErrClosedPipe
	}
}

func (b *leaseBlockedBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

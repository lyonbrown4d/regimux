package pypi

import (
	"context"
	"io"
	"net/http"
	"strings"
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
	synctest.Test(t, func(t *testing.T) {
		client := &http.Client{Transport: fillTransport(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("payload"))}, nil
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
			t.Fatal("idle owner retained a temporary spool")
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
	})
}

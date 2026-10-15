package pypi_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type stalledPackageStore struct {
	object.Store
	stallRead bool
}

func (s stalledPackageStore) Get(ctx context.Context, key string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	if !s.stallRead {
		body, info, err := s.Store.Get(ctx, key, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("get test object: %w", err)
		}
		return body, info, nil
	}
	<-ctx.Done()
	return nil, nil, fmt.Errorf("stalled object read: %w", ctx.Err())
}

func TestWheelMetadataUsesItsOwnContentIdentity(t *testing.T) {
	const metadataBody = "Metadata-Version: 2.1\nName: demo\nVersion: 1.0\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, metadataBody) }))
	defer upstream.Close()
	service := newTestService(t.Context(), t, upstream.URL, nil)
	req := pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl.metadata"), SHA256: strings.Repeat("0", 64)}
	for _, cache := range []string{"miss", "hit"} {
		resp, err := service.Get(t.Context(), req)
		requireNoError(t, "metadata response", err)
		if got := readResponse(t, resp); got != metadataBody || resp.Cache != cache {
			t.Fatalf("body=%q cache=%q", got, resp.Cache)
		}
	}
}

func (s stalledPackageStore) Put(ctx context.Context, key string, body io.Reader, opts object.PutOptions) (*object.Info, error) {
	if s.stallRead {
		info, err := s.Store.Put(ctx, key, body, opts)
		if err != nil {
			return nil, fmt.Errorf("put test object: %w", err)
		}
		return info, nil
	}
	<-ctx.Done()
	return nil, fmt.Errorf("stalled object write: %w", ctx.Err())
}

func TestPackageStorageStallsHaveIndependentBudget(t *testing.T) {
	for _, read := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "read"}[read], func(t *testing.T) { checkPackageStorageStall(t, read) })
	}
}

func checkPackageStorageStall(t *testing.T, stallRead bool) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, "healthy-wheel") }))
	defer upstream.Close()
	seed, metadata, objects := newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	req := pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.bin"), SkipPullRecord: true}
	if stallRead {
		resp, err := seed.Get(t.Context(), req)
		requireNoError(t, "seed", err)
		if got := readResponse(t, resp); got != "healthy-wheel" {
			t.Fatal(got)
		}
	}
	service := pypi.NewService(pypi.ServiceDependencies{Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}}, Metadata: metadata, Objects: stalledPackageStore{Store: objects, stallRead: stallRead}})
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	started := time.Now()
	resp, err := service.Get(ctx, req)
	requireNoError(t, "headers with stalled storage", err)
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("headers blocked by storage for %s", elapsed)
	}
	if got := readResponse(t, resp); got != "healthy-wheel" {
		t.Fatal(got)
	}
	if ctx.Err() != nil {
		t.Fatal("cache consumed caller deadline")
	}
}

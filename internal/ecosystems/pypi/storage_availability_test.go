package pypi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type stalledPackageStore struct {
	object.Store
	stallRead    bool
	writeStarted chan struct{}
	readCanceled *atomic.Int64
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
	if s.readCanceled != nil {
		s.readCanceled.Add(1)
	}
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
	if s.writeStarted != nil {
		close(s.writeStarted)
	}
	<-ctx.Done()
	return nil, fmt.Errorf("stalled object write: %w", ctx.Err())
}

func TestPackageStorageStallsHaveIndependentBudget(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, "healthy-wheel") }))
	defer upstream.Close()
	seed, metadata, objects := newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	req := pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.bin"), SkipPullRecord: true}
	resp, err := seed.Get(t.Context(), req)
	requireNoError(t, "seed", err)
	if got := readResponse(t, resp); got != "healthy-wheel" {
		t.Fatal(got)
	}
	var readCanceled atomic.Int64
	service := pypi.NewService(pypi.ServiceDependencies{Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}}, Metadata: metadata, Objects: stalledPackageStore{Store: objects, stallRead: true, readCanceled: &readCanceled}})
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	started := time.Now()
	resp, err = service.Get(ctx, req)
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
	if readCanceled.Load() == 0 {
		t.Fatal("test did not exercise a blocked object read canceled by its independent budget")
	}
}

func TestPackageCacheWriteHonorsParentCancellation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeResponse(t, w, "healthy-wheel") }))
	defer upstream.Close()
	_, metadata, objects := newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	started := make(chan struct{})
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: stalledPackageStore{Store: objects, writeStarted: started},
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := pypi.Request{Alias: "pypi", Tail: packageTailFor(t, upstream.URL, "/packages/demo.bin"), SkipPullRecord: true}
	resp, err := service.Get(ctx, req)
	requireNoError(t, "cold headers", err)
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "complete cold body", err)
	if string(body) != "healthy-wheel" {
		t.Fatalf("cold body=%q", body)
	}
	closed := make(chan error, 1)
	go func() { closed <- resp.Body.Close() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("cache write did not start")
	}
	cancel()
	select {
	case err := <-closed:
		requireNoError(t, "close canceled cache write", err)
	case <-time.After(time.Second):
		t.Fatal("cache write ignored parent cancellation")
	}
	assertCanceledPackageUnpublished(t, metadata, objects, req, body)
}

func assertCanceledPackageUnpublished(t *testing.T, metadata meta.Store, objects object.Store, req pypi.Request, body []byte) {
	t.Helper()
	route, err := pypi.ParseTail(req.Alias, req.Tail)
	requireNoError(t, "parse canceled route", err)
	_, found, err := metadata.Tag(t.Context(), meta.TagKey{Alias: route.Alias, Repository: route.Repository, Reference: route.Reference})
	if err != nil || found {
		t.Errorf("canceled cache write published tag: found=%t error=%v", found, err)
	}
	digest := sha256.Sum256(body)
	found, err = objects.Exists(t.Context(), "sha256:"+hex.EncodeToString(digest[:]))
	if err != nil || found {
		t.Errorf("canceled cache write published CAS: found=%t error=%v", found, err)
	}
}

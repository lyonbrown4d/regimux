package pypi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestPackageCacheWriteProgressOutlivesReadBudget(t *testing.T) {
	fixture := newCacheWriteFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cold, err := fixture.service.Get(ctx, fixture.req)
	requireNoError(t, "cold package", err)
	assertProgressWriteBody(t, cold, fixture.sha, fixture.size, "miss")
	closed := make(chan error, 1)
	go func() { closed <- cold.Body.Close() }()
	// This immediately arriving request must wait for the active fill, then hit it.
	warm, err := fixture.service.Get(ctx, fixture.req)
	requireNoError(t, "immediate warm package", err)
	defer closeBody(t, warm.Body)
	assertProgressWriteBody(t, warm, fixture.sha, fixture.size, "hit")
	requireNoError(t, "finish cache publication and release fill", <-closed)
	fixture.assertWrites(t)
}

func TestPackageCacheWriteProgressImmediatelyHitsHTTP(t *testing.T) {
	for _, adapter := range []struct {
		name  string
		start func(*testing.T, *pypi.Service) (*http.Client, string)
	}{
		{name: "std", start: startStdProxy},
		{name: "fiber", start: startFiberProxy},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			fixture := newCacheWriteFixture(t)
			client, proxyURL := adapter.start(t, fixture.service)
			client.Timeout = 20 * time.Second
			requestURL := proxyURL + "/pypi/pypi/" + fixture.req.Tail + "?__regimux_sha256=" + fixture.sha
			for _, cache := range []string{"miss", "hit"} {
				fixture.assertHTTPDownload(t, client, requestURL, cache)
			}
			fixture.assertWrites(t)
		})
	}
}

type cacheWriteFixture struct {
	service *pypi.Service
	req     pypi.Request
	sha     string
	size    int64
	calls   *atomic.Int64
	store   *progressingPackageWrites
}

func (f *cacheWriteFixture) assertHTTPDownload(t *testing.T, client *http.Client, requestURL, cache string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, requestURL, http.NoBody)
	requireNoError(t, "create wheel request", err)
	response, err := client.Do(request)
	requireNoError(t, "download wheel", err)
	defer closeBody(t, response.Body)
	assertProgressWriteBody(t, &pypi.Response{
		Status: response.StatusCode, Cache: response.Header.Get("X-Mirror-Cache"), Body: response.Body,
	}, f.sha, f.size, cache)
}

func newCacheWriteFixture(t *testing.T) *cacheWriteFixture {
	t.Helper()
	body := bytes.Repeat([]byte("complete-wheel!\n"), 8192)
	digest := sha256.Sum256(body)
	wantSHA := hex.EncodeToString(digest[:])
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		writeResponse(t, w, string(body))
	}))
	t.Cleanup(upstream.Close)
	_, metadata, objects := newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
	route, err := pypi.ParseTail("pypi", tail)
	requireNoError(t, "parse package route", err)
	store := &progressingPackageWrites{
		Store: objects, t: t, metadata: metadata, digest: "sha256:" + wantSHA,
		key: meta.TagKey{Alias: route.Alias, Repository: route.Repository, Reference: route.Reference},
	}
	service := pypi.NewService(pypi.ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}},
		Metadata: metadata, Objects: store,
	})
	return &cacheWriteFixture{
		service: service, req: pypi.Request{Alias: "pypi", Tail: tail, SHA256: wantSHA, SkipPullRecord: true},
		sha: wantSHA, size: int64(len(body)), calls: &calls, store: store,
	}
}

func (f *cacheWriteFixture) assertWrites(t *testing.T) {
	t.Helper()
	if f.calls.Load() != 1 {
		t.Errorf("upstream GETs=%d, want 1", f.calls.Load())
	}
	if f.store.completed.Load() != 1 || f.store.canceled.Load() != 0 {
		t.Errorf("cache writes completed=%d canceled=%d, want 1/0", f.store.completed.Load(), f.store.canceled.Load())
	}
}

func assertProgressWriteBody(t *testing.T, resp *pypi.Response, wantSHA string, wantSize int64, cache string) {
	t.Helper()
	hash := sha256.New()
	size, err := io.Copy(hash, resp.Body)
	requireNoError(t, "read complete package", err)
	if resp.Status != http.StatusOK || resp.Cache != cache || size != wantSize || hex.EncodeToString(hash.Sum(nil)) != wantSHA {
		t.Errorf("package status=%d cache=%s size=%d SHA=%x, want 200/%s/%d/%s", resp.Status, resp.Cache, size, hash.Sum(nil), cache, wantSize, wantSHA)
	}
}

type progressingPackageWrites struct {
	object.Store
	t         *testing.T
	metadata  meta.Store
	key       meta.TagKey
	digest    string
	completed atomic.Int64
	canceled  atomic.Int64
}

func (s *progressingPackageWrites) Put(ctx context.Context, key string, body io.Reader, opts object.PutOptions) (*object.Info, error) {
	reader := &progressingCacheReader{
		ctx: ctx, source: body, tick: time.NewTicker(70 * time.Millisecond),
		checkPartial: func() { s.assertUnpublished(ctx) },
	}
	defer reader.tick.Stop()
	info, err := s.Store.Put(ctx, key, reader, opts)
	if err != nil {
		s.canceled.Add(1)
		return nil, fmt.Errorf("write progressing package cache: %w", err)
	}
	s.completed.Add(1)
	return info, nil
}

func (s *progressingPackageWrites) assertUnpublished(ctx context.Context) {
	s.t.Helper()
	_, found, err := s.metadata.Tag(ctx, s.key)
	if err != nil || found {
		s.t.Errorf("incomplete cache write published tag: found=%t error=%v", found, err)
	}
	_, found, err = s.metadata.Blob(ctx, meta.BlobKey{Digest: s.digest})
	if err != nil || found {
		s.t.Errorf("incomplete cache write published blob: found=%t error=%v", found, err)
	}
	found, err = s.Exists(ctx, s.digest)
	if err != nil || found {
		s.t.Errorf("incomplete cache write published CAS object: found=%t error=%v", found, err)
	}
}

type progressingCacheReader struct {
	ctx          context.Context
	source       io.Reader
	tick         *time.Ticker
	reads        int
	checkPartial func()
}

func (r *progressingCacheReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, fmt.Errorf("progressing cache copy canceled: %w", r.ctx.Err())
	case <-r.tick.C:
	}
	n, err := r.source.Read(p[:min(len(p), 4096)])
	r.reads++
	if r.reads == 8 {
		r.checkPartial()
	}
	if err == io.EOF {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read progressing cache copy: %w", err)
	}
	return n, nil
}

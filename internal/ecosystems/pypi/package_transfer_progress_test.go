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
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

const (
	progressWheelSize   = 10 << 20
	progressWheelPrefix = 8 << 20
	progressWheelChunk  = 64 << 10
	progressWheelETag   = `"progress-wheel-v1"`
)

func TestPackageTransferProgressOutlivesHeaderAttemptTimeout(t *testing.T) {
	runPackageTransferAdapters(t, false)
}

func TestPackageTransferResumesInterruptedWheelWithoutPublishingPartialCache(t *testing.T) {
	runPackageTransferAdapters(t, true)
}

func TestPackageTransferResumesInterruptedWarmCache(t *testing.T) {
	for _, adapter := range []struct {
		name  string
		start func(*testing.T, *pypi.Service) (*http.Client, string)
	}{
		{name: "std", start: startStdProxy},
		{name: "fiber", start: startFiberProxy},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			fixture, seedService, upstream := newPackageTransferFixture(t, false)
			tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
			seed, err := seedService.Get(t.Context(), pypi.Request{Alias: "pypi", Tail: tail, SkipPullRecord: true})
			requireNoError(t, "seed warm wheel", err)
			_, err = io.Copy(io.Discard, seed.Body)
			requireNoError(t, "seed complete wheel body", err)
			closeBody(t, seed.Body)
			objects := &interruptedPackageObjects{Store: fixture.objects, t: t}
			service := pypi.NewService(pypi.ServiceDependencies{
				Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: upstream.URL}}},
				Metadata: fixture.metadata, Objects: objects,
			})
			client, proxyURL := adapter.start(t, service)
			client.Timeout = 10 * time.Second
			verifyPackageTransfer(t, client, proxyURL+"/pypi/pypi/"+tail, fixture, "hit")
			if objects.ranges.Load() != 1 || fixture.calls.Load() != 1 {
				t.Errorf("warm recovery ranges=%d upstream calls=%d, want 1 each", objects.ranges.Load(), fixture.calls.Load())
			}
		})
	}
}

type interruptedPackageObjects struct {
	object.Store
	t      *testing.T
	ranges atomic.Int64
}

func (s *interruptedPackageObjects) Get(ctx context.Context, key string, opts object.GetOptions) (io.ReadCloser, *object.Info, error) {
	body, info, err := s.Store.Get(ctx, key, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("open interrupted test object: %w", err)
	}
	if opts.Range != nil {
		s.ranges.Add(1)
		if opts.Range.Start != progressWheelPrefix || opts.Range.End != progressWheelSize-1 {
			s.t.Errorf("warm resume object range=%#v", opts.Range)
		}
		return body, info, nil
	}
	return struct {
		io.Reader
		io.Closer
	}{Reader: io.LimitReader(body, progressWheelPrefix), Closer: body}, info, nil
}

func runPackageTransferAdapters(t *testing.T, interrupt bool) {
	t.Helper()
	for _, adapter := range []struct {
		name  string
		start func(*testing.T, *pypi.Service) (*http.Client, string)
	}{
		{name: "std", start: startStdProxy},
		{name: "fiber", start: startFiberProxy},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			fixture, service, upstream := newPackageTransferFixture(t, interrupt)
			client, proxyURL := adapter.start(t, service)
			client.Timeout = 10 * time.Second
			tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
			query := url.Values{"__regimux_project": {"demo"}, "__regimux_sha256": {fixture.digest}}
			requestURL := proxyURL + "/pypi/pypi/" + tail + "?" + query.Encode()
			verifyPackageTransfer(t, client, requestURL, fixture, "miss")
			waitPackageTransferCache(t, fixture)
			verifyPackageTransfer(t, client, requestURL, fixture, "hit")
			assertPackageTransferRequests(t, fixture, interrupt)
		})
	}
}

func assertPackageTransferRequests(t *testing.T, fixture *packageTransferFixture, interrupt bool) {
	t.Helper()
	wantCalls := int64(1)
	if interrupt {
		wantCalls++
		if fixture.ranges.Load() != 1 {
			t.Errorf("resume Range requests=%d, want 1", fixture.ranges.Load())
		}
	}
	if fixture.calls.Load() != wantCalls {
		t.Errorf("upstream requests=%d, want %d including warm-cache request", fixture.calls.Load(), wantCalls)
	}
}

type packageTransferFixture struct {
	t         *testing.T
	body      []byte
	digest    string
	metadata  meta.Store
	objects   object.Store
	key       meta.TagKey
	interrupt bool
	calls     atomic.Int64
	ranges    atomic.Int64
}

func newPackageTransferFixture(t *testing.T, interrupt bool) (*packageTransferFixture, *pypi.Service, *httptest.Server) {
	t.Helper()
	body := make([]byte, progressWheelSize)
	for i := range body {
		body[i] = byte(i % 251)
	}
	digest := sha256.Sum256(body)
	fixture := &packageTransferFixture{t: t, body: body, digest: hex.EncodeToString(digest[:]), interrupt: interrupt}
	upstream := httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(upstream.Close)
	tail := packageTailFor(t, upstream.URL, "/packages/demo-1.0-py3-none-any.whl")
	fixture.key = meta.TagKey{Alias: "pypi", Repository: "pypi/packages", Reference: tail[len("packages/"):]}
	_, fixture.metadata, fixture.objects = newTestServiceWithStores(t.Context(), t, upstream.URL, nil)
	service := pypi.NewService(pypi.ServiceDependencies{
		Config: config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {
			Registry: upstream.URL, HTTP: config.HTTPConfig{AttemptTimeout: 100 * time.Millisecond},
		}}},
		Metadata: fixture.metadata, Objects: fixture.objects,
	})
	return fixture, service, upstream
}

func (f *packageTransferFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	w.Header().Set("ETag", progressWheelETag)
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.Header.Get("Range") != "" {
		f.serveResume(w, r)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(f.body)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(f.body[:progressWheelPrefix]); err != nil {
		f.t.Errorf("write initial wheel prefix: %v", err)
		return
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		f.t.Errorf("flush initial wheel prefix: %v", err)
		return
	}
	if f.interrupt {
		// Returning with an incomplete declared Content-Length closes the socket.
		return
	}
	f.serveSlowTail(w, r)
}

func (f *packageTransferFixture) serveSlowTail(w http.ResponseWriter, r *http.Request) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for offset := progressWheelPrefix; offset < len(f.body); offset += progressWheelChunk {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
		if _, err := w.Write(f.body[offset:min(offset+progressWheelChunk, len(f.body))]); err != nil {
			f.t.Errorf("write progressing wheel tail: %v", err)
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			f.t.Errorf("flush progressing wheel tail: %v", err)
			return
		}
	}
}

func (f *packageTransferFixture) serveResume(w http.ResponseWriter, r *http.Request) {
	f.ranges.Add(1)
	wantRange := "bytes=" + strconv.Itoa(progressWheelPrefix) + "-" + strconv.Itoa(len(f.body)-1)
	if r.Header.Get("Range") != wantRange || r.Header.Get("If-Range") != progressWheelETag {
		f.t.Errorf("resume Range=%q If-Range=%q, want %q and %q", r.Header.Get("Range"), r.Header.Get("If-Range"), wantRange, progressWheelETag)
		http.Error(w, "incorrect resume request", http.StatusBadRequest)
		return
	}
	f.assertNoPartialCache(r.Context())
	http.ServeContent(w, r, "demo-1.0-py3-none-any.whl", time.Unix(1, 0), bytes.NewReader(f.body))
}

func (f *packageTransferFixture) assertNoPartialCache(ctx context.Context) {
	_, found, err := f.metadata.Tag(ctx, f.key)
	if err != nil || found {
		f.t.Errorf("partial transfer published package tag: found=%t error=%v", found, err)
	}
	partialHash := sha256.Sum256(f.body[:progressWheelPrefix])
	partialDigest := "sha256:" + hex.EncodeToString(partialHash[:])
	_, found, err = f.metadata.Blob(ctx, meta.BlobKey{Digest: partialDigest})
	if err != nil || found {
		f.t.Errorf("partial transfer published blob metadata: found=%t error=%v", found, err)
	}
	found, err = f.objects.Exists(ctx, partialDigest)
	if err != nil || found {
		f.t.Errorf("partial transfer published CAS object: found=%t error=%v", found, err)
	}
}

func verifyPackageTransfer(t *testing.T, client *http.Client, requestURL string, fixture *packageTransferFixture, cache string) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, requestURL, http.NoBody)
	requireNoError(t, "build wheel request", err)
	response, err := client.Do(request)
	requireNoError(t, "fetch wheel headers", err)
	defer closeBody(t, response.Body)
	digest := sha256.New()
	size, err := io.Copy(digest, response.Body)
	if err != nil || size != int64(len(fixture.body)) {
		fixture.assertNoPartialCache(t.Context())
		t.Fatalf("wheel transfer bytes=%d, want %d, error=%v", size, len(fixture.body), err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Mirror-Cache") != cache {
		t.Errorf("wheel status=%d cache=%q, want 200/%s", response.StatusCode, response.Header.Get("X-Mirror-Cache"), cache)
	}
	if actual := hex.EncodeToString(digest.Sum(nil)); actual != fixture.digest {
		t.Errorf("wheel SHA-256=%s, want %s", actual, fixture.digest)
	}
}

func waitPackageTransferCache(t *testing.T, fixture *packageTransferFixture) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		tag, found, err := fixture.metadata.Tag(t.Context(), fixture.key)
		requireNoError(t, "lookup completed wheel cache", err)
		if found {
			if tag.Digest != "sha256:"+fixture.digest {
				t.Fatalf("completed wheel cache digest=%q", tag.Digest)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("complete wheel transfer did not publish its cache")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

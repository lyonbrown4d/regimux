package pypi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/spool"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

type failingSimpleIndexObjectStore struct{ object.Store }

type countingIndexReader struct {
	reader io.Reader
	reads  int
}

func (r *countingIndexReader) Read(data []byte) (int, error) {
	r.reads++
	n, readErr := r.reader.Read(data)
	if errors.Is(readErr, io.EOF) {
		return n, io.EOF
	}
	if readErr != nil {
		return n, fmt.Errorf("read counting index body: %w", readErr)
	}
	return n, nil
}

func (failingSimpleIndexObjectStore) Put(context.Context, string, io.Reader, object.PutOptions) (*object.Info, error) {
	return nil, errors.New("object write failed")
}

func TestAcquireMetadataSlotHonorsCancellationAndRelease(t *testing.T) {
	slots := make(chan struct{}, 1)
	release, err := acquireMetadataSlot(context.Background(), slots)
	if err != nil {
		t.Fatalf("acquire first slot: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireMetadataSlot(ctx, slots); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire canceled slot error = %v, want context.Canceled", err)
	}
	release()
	release()
	if _, err := acquireMetadataSlot(context.Background(), slots); err != nil {
		t.Fatalf("acquire released slot: %v", err)
	}
}

func TestSimpleIndexAdmissionCancellationDoesNotReadBodyAndReleasesSlot(t *testing.T) {
	slots := make(chan struct{}, metadataTransformConcurrency)
	releases := fillIndexAdmissionSlots(t, slots)
	service := &Service{metadataSlots: slots}
	source := &countingIndexReader{reader: strings.NewReader("index")}
	called := false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if service.withSimpleIndexAdmission(ctx, source, func([]byte) { called = true }) {
		t.Fatal("canceled admission succeeded")
	}
	if source.reads != 0 || called {
		t.Fatalf("canceled admission read body %d times and called parser %t", source.reads, called)
	}
	if got := len(slots); got != metadataTransformConcurrency {
		t.Fatalf("canceled admission changed occupied slots to %d", got)
	}
	releaseMetadataSlots(releases)
	if !service.withSimpleIndexAdmission(context.Background(), source, func(body []byte) {
		called = string(body) == "index"
	}) {
		t.Fatal("admission failed after slots were released")
	}
	if !called || source.reads == 0 {
		t.Fatalf("successful admission read body %d times and parser result was %t", source.reads, called)
	}
	if got := len(slots); got != 0 {
		t.Fatalf("successful admission retained %d slots", got)
	}
}

func fillIndexAdmissionSlots(t *testing.T, slots chan struct{}) []func() {
	t.Helper()
	releases := make([]func(), 0, metadataTransformConcurrency)
	for range metadataTransformConcurrency {
		release, err := acquireMetadataSlot(t.Context(), slots)
		if err != nil {
			t.Fatalf("fill metadata slots: %v", err)
		}
		releases = append(releases, release)
	}
	return releases
}

func TestSimpleIndexResponseReleasesAdmissionAfterSpooling(t *testing.T) {
	service := &Service{logger: slog.Default(), metadataSlots: make(chan struct{}, metadataTransformConcurrency)}
	route := Route{Alias: "pypi", Kind: RouteSimple, Repository: "simple/pkg", Reference: "index"}
	newFetch := func() *upstreamFetch {
		return &upstreamFetch{
			status:     http.StatusOK,
			headers:    http.Header{"Content-Type": {"text/html"}},
			body:       io.NopCloser(strings.NewReader("<html><body>index</body></html>")),
			requestURL: "https://pypi.org/simple/pkg/",
		}
	}
	first, err := service.responseFromFetched(context.Background(), Request{}, route, newFetch())
	if err != nil {
		t.Fatalf("first index response: %v", err)
	}
	second, err := service.responseFromFetched(context.Background(), Request{}, route, newFetch())
	if err != nil {
		t.Fatalf("second index response: %v", err)
	}
	assertAdmissionSlotsReleased(t, len(service.metadataSlots))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, canceledErr := service.responseFromFetched(ctx, Request{}, route, newFetch()); !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("canceled third index error = %v, want context.Canceled", canceledErr)
	}
	third, err := service.responseFromFetched(context.Background(), Request{}, route, newFetch())
	if err != nil {
		t.Fatalf("third index after release: %v", err)
	}
	assertSimpleIndexResponses(t, first, second, third)
	for _, response := range []*Response{first, second, third} {
		if err := response.Body.Close(); err != nil {
			t.Fatalf("close index body: %v", err)
		}
	}
}

func releaseMetadataSlots(releases []func()) {
	for _, release := range releases {
		release()
	}
}

func assertAdmissionSlotsReleased(t *testing.T, occupied int) {
	t.Helper()
	if occupied != 0 {
		t.Fatalf("metadata slots retained after spooling = %d, want 0", occupied)
	}
}

func assertSimpleIndexResponses(t *testing.T, responses ...*Response) {
	t.Helper()
	for i, response := range responses {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read index response %d: %v", i, readErr)
		}
		if !bytes.Contains(body, []byte("<html><body>index</body></html>")) {
			t.Fatalf("index response %d body = %q, missing original HTML", i, body)
		}
	}
}

func TestPrepareSimpleIndexAcceptsNilHeaders(t *testing.T) {
	service := &Service{logger: slog.Default()}
	prepared, err := service.prepareFetched(
		Request{},
		Route{Alias: "pypi", Kind: RouteSimple},
		&upstreamFetch{
			status:     http.StatusOK,
			body:       io.NopCloser(strings.NewReader("<html></html>")),
			requestURL: "https://pypi.org/simple/pkg/",
		},
	)
	if err != nil {
		t.Fatalf("prepare simple index with nil headers: %v", err)
	}
	tmp, ok := prepared.body.(*spool.TempBody)
	if !ok {
		t.Fatalf("prepared body type = %T, want spooled temp body", prepared.body)
	}
	defer func() {
		if closeErr := tmp.Close(); closeErr != nil {
			t.Errorf("close spooled index: %v", closeErr)
		}
	}()
	if prepared.headers.Get("X-Regimux-Metadata-Format") != pypiSimpleIndexFormat {
		t.Fatalf("metadata format header = %q", prepared.headers.Get("X-Regimux-Metadata-Format"))
	}
}

func TestSimpleIndexCacheFailureRewindsSpoolForResponse(t *testing.T) {
	ctx := context.Background()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "metadata.db")})
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata store: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open object store: %v", err)
	}
	service := NewService(ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: "https://pypi.org"}}},
		Metadata: metadata,
		Objects:  failingSimpleIndexObjectStore{Store: objects},
		Logger:   slog.Default(),
	})
	route := Route{Alias: "pypi", Kind: RouteSimple, Repository: "simple/pkg", Reference: "index"}
	response, err := service.responseFromFetched(ctx, Request{}, route, &upstreamFetch{
		status:     http.StatusOK,
		headers:    http.Header{"Content-Type": {"text/html"}},
		body:       io.NopCloser(strings.NewReader("<html><body>fallback</body></html>")),
		requestURL: "https://pypi.org/simple/pkg/",
	})
	if err != nil {
		t.Fatalf("simple index response after cache failure: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read fallback body: %v", err)
	}
	if !bytes.Contains(body, []byte("<html><body>fallback</body></html>")) {
		t.Fatalf("fallback body = %q", body)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close fallback body: %v", err)
	}
}

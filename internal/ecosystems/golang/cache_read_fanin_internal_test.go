package golang

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/testkit"
)

func TestServiceCoalescesWarmGoCacheReads(t *testing.T) {
	fixture := newGoWarmFixture(t)
	warmGoCache(t, fixture)
	makeGoCacheTouchDue(t, fixture)
	fixture.metadata.resetCounts()
	started, release := fixture.metadata.armTagGate(false, false)
	defer release()
	results := testkit.StartConcurrent(goWarmReaders, func() (*Response, error) {
		return fixture.service.Get(t.Context(), fixture.request)
	}).WithTimeout(30 * time.Second)
	finished := false
	defer func() {
		release()
		if !finished {
			_ = results.Wait(t)
		}
	}()
	waitForGoSignal(t, started)
	waitForGoCacheReaders(t, fixture.metadata, goWarmReaders)
	blockedCounts := fixture.metadata.counts()
	if blockedCounts.manifests != 0 || blockedCounts.blobs != 0 || blockedCounts.touches != 0 {
		t.Fatalf("metadata calls while first tag is blocked = %d/%d/%d/%d, want no manifest or access calls", blockedCounts.tags, blockedCounts.manifests, blockedCounts.blobs, blockedCounts.touches)
	}
	release()
	responses := results.Wait(t)
	finished = true
	assertGoWarmCounts(t, fixture.metadata.counts(), 1)
	assertGoWarmResponses(t, responses, fixture.body)
	if got := fixture.requests.Load(); got != 1 {
		t.Fatalf("upstream requests after warm reads = %d, want 1", got)
	}
}

func TestServiceMetadataWriteInvalidatesGoReadSnapshot(t *testing.T) {
	metadata, objects := newGoReadStores(t)
	service := NewService(ServiceDependencies{Metadata: metadata, Objects: objects})
	requestRoute, err := parseRoute("default", "github.com/acme/lib/@v/v1.2.3.mod")
	requireGoCacheNoError(t, "parse Go route", err)

	oldBody := []byte("old module body")
	oldInfo := putGoTestObject(t, objects, oldBody)
	if err := service.storeMetadata(t.Context(), requestRoute, oldInfo.Digest, oldInfo, http.Header{"ETag": {"old"}}, "text/plain"); err != nil {
		t.Fatalf("store old Go metadata: %v", err)
	}

	started, release := metadata.armTagGate(true, true)
	defer release()
	oldResult := startGoCacheRead(t.Context(), service, requestRoute)
	waitForGoSignal(t, started)

	newBody := []byte("new module body")
	newInfo := putGoTestObject(t, objects, newBody)
	if err := service.storeMetadata(t.Context(), requestRoute, newInfo.Digest, newInfo, http.Header{"ETag": {"new"}}, "text/plain"); err != nil {
		t.Fatalf("store new Go metadata: %v", err)
	}
	newResult := startGoCacheRead(t.Context(), service, requestRoute)
	current := waitForGoCacheRead(t, newResult)
	assertGoCachedBody(t, current, newBody, "new")

	release()
	previous := waitForGoCacheRead(t, oldResult)
	assertGoCachedBody(t, previous, oldBody, "old")
}

func newGoReadStores(t *testing.T) (*countGoCacheMetadata, object.Store) {
	t.Helper()
	metadataDB, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "metadata.db")})
	requireGoCacheNoError(t, "open metadata", err)
	t.Cleanup(func() { requireGoCacheNoError(t, "close metadata", metadataDB.Close()) })
	objects, err := object.NewLocal(t.TempDir())
	requireGoCacheNoError(t, "open objects", err)
	return &countGoCacheMetadata{Store: metadataDB, access: metadataDB}, objects
}

func putGoTestObject(t *testing.T, objects object.Store, body []byte) *object.Info {
	t.Helper()
	digest := goBodyDigest(body)
	info, err := objects.Put(t.Context(), digest, bytes.NewReader(body), object.PutOptions{ContentType: "text/plain"})
	requireGoCacheNoError(t, "put Go test object", err)
	return info
}

func startGoCacheRead(ctx context.Context, service *Service, route route) <-chan goCachedResult {
	result := make(chan goCachedResult, 1)
	go func() {
		cached, found, err := service.cached(ctx, route)
		result <- goCachedResult{response: cached, found: found, err: err}
	}()
	return result
}

func waitForGoCacheRead(t *testing.T, result <-chan goCachedResult) goCachedResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("Go cache read did not finish after metadata write")
		return goCachedResult{}
	}
}

func assertGoCachedBody(t *testing.T, result goCachedResult, want []byte, etag string) {
	t.Helper()
	if result.err != nil {
		t.Fatalf("read Go cache: %v", result.err)
	}
	if !result.found {
		t.Fatal("Go cache entry was not found")
	}
	body, err := io.ReadAll(result.response.body)
	closeErr := result.response.body.Close()
	if err != nil || closeErr != nil || !bytes.Equal(body, want) {
		t.Fatalf("Go cached body = %q, read error = %v, close error = %v; want %q", body, err, closeErr, want)
	}
	if got := result.response.headers.Get("ETag"); got != etag {
		t.Fatalf("Go cached ETag = %q, want %q", got, etag)
	}
}

func requireGoCacheNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

type goCachedResult struct {
	response storedResponse
	found    bool
	err      error
}

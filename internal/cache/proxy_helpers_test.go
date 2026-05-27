package cache_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/cache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func newTestStores(t *testing.T) (meta.Store, object.Store) {
	t.Helper()
	metadata, err := meta.OpenBbolt(t.TempDir()+"/regimux.db", nil)
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Fatalf("close metadata store: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open object store: %v", err)
	}
	return metadata, objects
}

func readAndClose(t *testing.T, reader io.ReadCloser) []byte {
	t.Helper()
	body, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close reader: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

func assertRangeBlobMiss(t *testing.T, result *cache.BlobReadResult) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheMiss || result.Status != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("unexpected range result: cache=%s status=%d body=%q", result.Cache, result.Status, body)
	}
	if got := result.Headers.Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("unexpected content range %q", got)
	}
	if got := result.Headers.Get("Content-Length"); got != "4" {
		t.Fatalf("unexpected content length %q", got)
	}
}

func assertRangeBlobBypass(t *testing.T, result *cache.BlobReadResult) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheBypass || result.Status != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("unexpected range result: cache=%s status=%d body=%q", result.Cache, result.Status, body)
	}
	if got := result.Headers.Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("unexpected content range %q", got)
	}
	if got := result.Headers.Get("Content-Length"); got != "4" {
		t.Fatalf("unexpected content length %q", got)
	}
}

func assertRangeBlobHit(t *testing.T, result *cache.BlobReadResult) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheHit || result.Status != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("unexpected range result: cache=%s status=%d body=%q", result.Cache, result.Status, body)
	}
	if got := result.Headers.Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("unexpected content range %q", got)
	}
	if got := result.Headers.Get("Content-Length"); got != "4" {
		t.Fatalf("unexpected content length %q", got)
	}
}

func assertFullBlobHit(t *testing.T, result *cache.BlobReadResult, want []byte) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheHit || !bytes.Equal(body, want) {
		t.Fatalf("unexpected full result: cache=%s body=%q", result.Cache, body)
	}
}

func assertHeadBlobHit(t *testing.T, result *cache.BlobReadResult, wantSize int) {
	t.Helper()
	body := readAndClose(t, result.Reader)
	if result.Cache != cache.CacheHit || len(body) != 0 {
		t.Fatalf("unexpected head result: cache=%s body=%q", result.Cache, body)
	}
	if got := result.Headers.Get("Content-Length"); got != strconv.Itoa(wantSize) {
		t.Fatalf("unexpected head content length %q", got)
	}
}

func testDigestFor(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

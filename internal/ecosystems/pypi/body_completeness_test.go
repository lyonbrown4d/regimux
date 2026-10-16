//nolint:testpackage // White-box tests cover packageBody integrity invariants.
package pypi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestPackageBodyCompletesWhenKnownLengthIsReadExactly(t *testing.T) {
	bodyBytes := []byte("complete despite a nil-error final read")
	digest := sha256.Sum256(bodyBytes)
	service, route, body := newPackageBodyForCompletenessTest(context.Background(), t, bodyBytes, int64(len(bodyBytes)), hex.EncodeToString(digest[:]))
	if _, err := io.CopyN(io.Discard, body, int64(len(bodyBytes))); err != nil {
		t.Fatalf("copy exact declared size: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if _, ok, err := service.packageManifest(context.Background(), route); err != nil {
		t.Fatalf("read cached manifest: %v", err)
	} else if !ok {
		t.Fatal("complete package was not cached after reading exactly Content-Length")
	}
}

func TestPackageBodyRejectsWrongDigest(t *testing.T) {
	bodyBytes := []byte("complete despite a nil-error final read")
	service, route, body := newPackageBodyForCompletenessTest(context.Background(), t, bodyBytes, int64(len(bodyBytes)), strings.Repeat("0", 64))
	readBuffer := make([]byte, len(bodyBytes))
	if n, err := body.Read(readBuffer); n != len(bodyBytes) || err == nil {
		t.Fatalf("last read = (%d, %v), want all bytes and a digest error", n, err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if _, ok, err := service.packageManifest(context.Background(), route); err != nil {
		t.Fatalf("read cached manifest: %v", err)
	} else if ok {
		t.Fatal("package with a wrong expected digest was cached")
	}
}

func TestPackageBodyRejectsLengthOverrun(t *testing.T) {
	bodyBytes := []byte("complete despite a nil-error final read")
	digest := sha256.Sum256(bodyBytes)
	service, route, body := newPackageBodyForCompletenessTest(context.Background(), t, bodyBytes, int64(len(bodyBytes)-1), hex.EncodeToString(digest[:]))
	if _, err := io.ReadAll(body); err == nil {
		t.Fatal("read succeeded despite exceeding Content-Length")
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if _, ok, err := service.packageManifest(context.Background(), route); err != nil {
		t.Fatalf("read cached manifest: %v", err)
	} else if ok {
		t.Fatal("package exceeding Content-Length was cached")
	}
}

func TestPackageBodyDoesNotCacheAfterCancellation(t *testing.T) {
	bodyBytes := []byte("complete despite a nil-error final read")
	digest := sha256.Sum256(bodyBytes)
	ctx, cancel := context.WithCancel(context.Background())
	service, route, body := newPackageBodyForCompletenessTest(ctx, t, bodyBytes, int64(len(bodyBytes)), hex.EncodeToString(digest[:]))
	if _, err := io.CopyN(io.Discard, body, int64(len(bodyBytes))); err != nil {
		t.Fatalf("copy exact declared size: %v", err)
	}
	cancel()
	if err := body.Close(); err != nil {
		t.Fatalf("close canceled body: %v", err)
	}
	if _, ok, err := service.packageManifest(context.Background(), route); err != nil {
		t.Fatalf("read cached manifest: %v", err)
	} else if ok {
		t.Fatal("canceled package body was cached")
	}
}

type terminalNilReader struct {
	body []byte
	done bool
}

func (r *terminalNilReader) Read(dst []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	return copy(dst, r.body), nil
}

func newPackageBodyForCompletenessTest(ctx context.Context, t *testing.T, bodyBytes []byte, contentLength int64, expectedSHA string) (*Service, Route, *packageBody) {
	t.Helper()
	service, route := newBodyCompletenessService(ctx, t)
	wrapped := service.packageCacheBody(ctx, Request{SHA256: expectedSHA}, route, &upstreamFetch{
		body:    io.NopCloser(&terminalNilReader{body: bodyBytes}),
		headers: http.Header{"Content-Length": {strconv.FormatInt(contentLength, 10)}},
	}, nil)
	body, ok := wrapped.(*packageBody)
	if !ok {
		t.Fatal("packageCacheBody did not return packageBody")
	}
	return service, route, body
}

func newBodyCompletenessService(ctx context.Context, t *testing.T) (*Service, Route) {
	t.Helper()
	metadata, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	if err != nil {
		t.Fatalf("open metadata: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := metadata.Close(); closeErr != nil {
			t.Errorf("close metadata: %v", closeErr)
		}
	})
	objects, err := object.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("open object store: %v", err)
	}
	service := NewService(ServiceDependencies{
		Config:   config.Config{PyPI: config.DependencyEcosystemConfig{"pypi": {Registry: "https://pypi.example.test/simple"}}},
		Metadata: metadata,
		Objects:  objects,
	})
	route := Route{Alias: "pypi", Kind: RoutePackage, Repository: "packages", Reference: "https/example/demo.whl"}
	return service, route
}

package golang_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/golang"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func newTestService(ctx context.Context, t *testing.T, upstreamURL string) *golang.Service {
	t.Helper()
	return newTestServiceWithUpstreams(ctx, t, map[string]config.DependencyUpstreamConfig{
		"default": {Registry: upstreamURL},
	})
}

func newTestServiceWithUpstreams(ctx context.Context, t *testing.T, upstreams map[string]config.DependencyUpstreamConfig) *golang.Service {
	t.Helper()
	service, _ := newTestServiceWithMetadata(ctx, t, upstreams)
	return service
}

func newTestServiceWithMetadata(ctx context.Context, t *testing.T, upstreams map[string]config.DependencyUpstreamConfig) (*golang.Service, meta.Store) {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewMemory("go-test")
	requireNoError(t, "open objects", err)
	return golang.NewService(golang.ServiceDependencies{
		Config:   config.Config{Go: upstreams},
		Metadata: db,
		Objects:  objects,
	}), db
}

func assertBody(t *testing.T, resp *golang.Response, want string) {
	t.Helper()
	body := responseBody(t, resp)
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func responseBody(t *testing.T, resp *golang.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatalf("response body is empty")
	}
	defer closeBody(t, resp.Body)
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "read body", err)
	return string(body)
}

func writeResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func closeBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

func expireArtifactMetadata(ctx context.Context, t *testing.T, metadata meta.Store, alias, repo, reference string) {
	t.Helper()
	expiresAt := time.Now().UTC().Add(-time.Minute)
	tag, ok, err := metadata.Tag(ctx, meta.TagKey{Alias: alias, Repository: repo, Reference: reference})
	if err != nil || !ok {
		t.Fatalf("lookup tag for expiration: ok=%v err=%v", ok, err)
	}
	tag.ExpiresAt = expiresAt
	if _, updateErr := metadata.UpsertTag(ctx, *tag); updateErr != nil {
		t.Fatalf("expire tag: %v", updateErr)
	}
	manifest, ok, err := metadata.Manifest(ctx, meta.ManifestKey{Alias: alias, Repository: repo, Digest: tag.Digest})
	if err != nil || !ok {
		t.Fatalf("lookup manifest for expiration: ok=%v err=%v", ok, err)
	}
	manifest.ExpiresAt = expiresAt
	if _, updateErr := metadata.UpsertManifest(ctx, *manifest); updateErr != nil {
		t.Fatalf("expire manifest: %v", updateErr)
	}
}

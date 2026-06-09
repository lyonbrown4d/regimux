package maven_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/maven"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func newTestService(ctx context.Context, t *testing.T, upstreams map[string]config.DependencyUpstreamConfig) (*maven.Service, meta.Store) {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewMemory("maven-test")
	requireNoError(t, "open objects", err)
	return maven.NewService(maven.ServiceDependencies{
		Config:   config.Config{Maven: upstreams},
		Metadata: db,
		Objects:  objects,
	}), db
}

func assertTagExpires(ctx context.Context, t *testing.T, metadata meta.Store, key meta.TagKey) {
	t.Helper()
	tag, ok, err := metadata.Tag(ctx, key)
	requireNoError(t, "lookup tag", err)
	if !ok {
		t.Fatalf("tag %s was not stored", key.String())
	}
	if tag.ExpiresAt.IsZero() {
		t.Fatalf("tag %s expires_at is zero, want short TTL", key.String())
	}
}

func assertBody(t *testing.T, resp *maven.Response, want string) {
	t.Helper()
	body := responseBody(t, resp)
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func responseBody(t *testing.T, resp *maven.Response) string {
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
	if _, err := w.Write([]byte(body)); err != nil {
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

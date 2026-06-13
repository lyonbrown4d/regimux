package pypi_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/pypi"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func newTestService(ctx context.Context, t *testing.T, upstreamURL string, now func() time.Time) *pypi.Service {
	t.Helper()
	service, _, _ := newTestServiceWithStores(ctx, t, upstreamURL, now)
	return service
}

func newTestServiceWithStores(ctx context.Context, t *testing.T, upstreamURL string, now func() time.Time) (*pypi.Service, meta.Store, object.Store) {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewMemory("pypi-test")
	requireNoError(t, "open objects", err)
	return pypi.NewService(pypi.ServiceDependencies{
		Config: config.Config{
			PyPI: config.DependencyEcosystemConfig{
				"pypi": {Registry: upstreamURL},
			},
		},
		Metadata: db,
		Objects:  objects,
		Now:      now,
	}), db, objects
}

func upstreamPackageURL(r *http.Request, path string) string {
	return "http://" + r.Host + path
}

func expectedLocalHref(t *testing.T, alias, upstreamURL, path string) string {
	t.Helper()
	parsed, err := url.Parse(upstreamURL)
	requireNoError(t, "parse upstream url", err)
	return "/pypi/" + alias + "/packages/" + parsed.Scheme + "/" + parsed.Host + path
}

func packageTailFor(t *testing.T, upstreamURL, path string) string {
	t.Helper()
	parsed, err := url.Parse(upstreamURL)
	requireNoError(t, "parse upstream url", err)
	return "packages/" + parsed.Scheme + "/" + parsed.Host + "/" + strings.TrimLeft(path, "/")
}

func readResponse(t *testing.T, resp *pypi.Response) string {
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
	if _, err := strings.NewReader(body).WriteTo(w); err != nil {
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

func assertPolicyDeniedPull(ctx context.Context, t *testing.T, metadata meta.Store, key meta.PullKey) {
	t.Helper()
	pull, ok, err := metadata.Pull(ctx, key)
	requireNoError(t, "lookup policy denied pull", err)
	if !ok {
		t.Fatalf("policy denied pull %s was not recorded", key.String())
	}
	if pull.PolicyDeniedCount != 1 || pull.LastPolicyDeniedAt.IsZero() {
		t.Fatalf("unexpected policy denied pull: %#v", pull)
	}
	if pull.Count != 0 || !pull.LastPullAt.IsZero() || !pull.LastUpstreamPullAt.IsZero() {
		t.Fatalf("policy denied pull should not count as success: %#v", pull)
	}
}

func assertStoredArtifact(ctx context.Context, t *testing.T, metadata meta.Store, objects object.Store, key meta.TagKey, wantBody, wantMediaType string) {
	t.Helper()
	tag := requireStoredTag(ctx, t, metadata, key)
	manifest := requireStoredManifest(ctx, t, metadata, key, tag.Digest)
	assertStoredManifest(t, manifest, key, wantBody, wantMediaType)
	blob := requireStoredBlob(ctx, t, metadata, tag.Digest)
	if blob.ObjectKey != tag.Digest {
		t.Fatalf("blob object key = %q, want %q", blob.ObjectKey, tag.Digest)
	}
	repoBlob := requireStoredRepoBlob(ctx, t, metadata, key, tag.Digest)
	if repoBlob.SourceManifest != tag.Digest {
		t.Fatalf("repo blob source manifest = %q, want %q", repoBlob.SourceManifest, tag.Digest)
	}
	assertStoredObject(ctx, t, objects, manifest, wantBody)
}

func requireStoredTag(ctx context.Context, t *testing.T, metadata meta.Store, key meta.TagKey) *meta.TagRecord {
	t.Helper()
	tag, ok, err := metadata.Tag(ctx, key)
	requireNoError(t, "lookup stored tag", err)
	if !ok {
		t.Fatalf("tag %s was not stored", key.String())
	}
	return tag
}

func requireStoredManifest(ctx context.Context, t *testing.T, metadata meta.Store, key meta.TagKey, digest string) *meta.ManifestRecord {
	t.Helper()
	manifest, ok, err := metadata.Manifest(ctx, meta.ManifestKey{Alias: key.Alias, Repository: key.Repository, Digest: digest})
	requireNoError(t, "lookup stored manifest", err)
	if !ok {
		t.Fatalf("manifest %s/%s@%s was not stored", key.Alias, key.Repository, digest)
	}
	return manifest
}

func assertStoredManifest(t *testing.T, manifest *meta.ManifestRecord, key meta.TagKey, wantBody, wantMediaType string) {
	t.Helper()
	if manifest.Reference != key.Reference {
		t.Fatalf("manifest reference = %q, want %q", manifest.Reference, key.Reference)
	}
	if manifest.MediaType != wantMediaType {
		t.Fatalf("manifest media type = %q, want %q", manifest.MediaType, wantMediaType)
	}
	if manifest.Size != int64(len(wantBody)) {
		t.Fatalf("manifest size = %d, want %d", manifest.Size, len(wantBody))
	}
}

func requireStoredBlob(ctx context.Context, t *testing.T, metadata meta.Store, digest string) *meta.BlobRecord {
	t.Helper()
	blob, ok, err := metadata.Blob(ctx, meta.BlobKey{Digest: digest})
	requireNoError(t, "lookup stored blob", err)
	if !ok {
		t.Fatalf("blob %s was not stored", digest)
	}
	return blob
}

func requireStoredRepoBlob(ctx context.Context, t *testing.T, metadata meta.Store, key meta.TagKey, digest string) *meta.RepoBlobRecord {
	t.Helper()
	repoBlob, ok, err := metadata.RepoBlob(ctx, meta.RepoBlobKey{Alias: key.Alias, Repository: key.Repository, Digest: digest})
	requireNoError(t, "lookup stored repo blob", err)
	if !ok {
		t.Fatalf("repo blob %s/%s@%s was not stored", key.Alias, key.Repository, digest)
	}
	return repoBlob
}

func assertStoredObject(ctx context.Context, t *testing.T, objects object.Store, manifest *meta.ManifestRecord, wantBody string) {
	t.Helper()
	objectKey := manifest.ObjectKey
	if objectKey == "" {
		objectKey = manifest.Digest
	}
	reader, info, err := objects.Get(ctx, objectKey, object.GetOptions{})
	requireNoError(t, "open stored object", err)
	defer closeBody(t, reader)
	body, err := io.ReadAll(reader)
	requireNoError(t, "read stored object", err)
	if string(body) != wantBody {
		t.Fatalf("stored object body = %q, want %q", string(body), wantBody)
	}
	if info == nil || info.Size != int64(len(wantBody)) {
		t.Fatalf("stored object size = %v, want %d", info, len(wantBody))
	}
}

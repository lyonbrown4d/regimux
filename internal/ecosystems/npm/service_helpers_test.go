package npm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/npm"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func newTestService(ctx context.Context, t *testing.T, upstreamURL string, metadataTTL time.Duration) *npm.Service {
	t.Helper()
	service, _ := newTestServiceWithMetadata(ctx, t, upstreamURL, metadataTTL)
	return service
}

func newTestServiceWithMetadata(ctx context.Context, t *testing.T, upstreamURL string, metadataTTL time.Duration) (*npm.Service, meta.Store) {
	t.Helper()
	service, metadata, _ := newTestServiceWithStores(ctx, t, upstreamURL, metadataTTL)
	return service, metadata
}

func newTestServiceWithStores(ctx context.Context, t *testing.T, upstreamURL string, metadataTTL time.Duration) (*npm.Service, meta.Store, object.Store) {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open objects", err)
	return npm.NewService(npm.ServiceDependencies{
		Config: config.Config{
			NPM: config.DependencyEcosystemConfig{
				"npmjs": {Registry: upstreamURL},
			},
		},
		Metadata:    db,
		Objects:     objects,
		MetadataTTL: metadataTTL,
	}), db, objects
}

func readJSON(t *testing.T, resp *npm.Response) map[string]any {
	t.Helper()
	body := readBody(t, resp)
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("decode response json: %v\nbody: %s", err, body)
	}
	return doc
}

func readBody(t *testing.T, resp *npm.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatal("response body is empty")
	}
	defer closeTestBody(t, resp.Body)
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "read body", err)
	return string(body)
}

func assertTarball(t *testing.T, doc map[string]any, version, want string) {
	t.Helper()
	versions, ok := doc["versions"].(map[string]any)
	if !ok {
		t.Fatalf("versions missing in %#v", doc)
	}
	versionDoc, ok := versions[version].(map[string]any)
	if !ok {
		t.Fatalf("version %s missing in %#v", version, versions)
	}
	dist, ok := versionDoc["dist"].(map[string]any)
	if !ok {
		t.Fatalf("dist missing in %#v", versionDoc)
	}
	if got := dist["tarball"]; got != want {
		t.Fatalf("tarball = %q, want %q", got, want)
	}
}

func upstreamTarballURL(r *http.Request, tail string) string {
	return "https://" + r.Host + "/" + tail
}

func writeResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := strings.NewReader(body).WriteTo(w); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func closeTestBody(t *testing.T, body io.Closer) {
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

func expireArtifactMetadata(ctx context.Context, t *testing.T, metadata meta.Store, alias, repo, reference string) {
	t.Helper()
	expiresAt := time.Now().UTC().Add(-time.Minute)
	tag := requireTagForExpiration(ctx, t, metadata, alias, repo, reference)
	tag.ExpiresAt = expiresAt
	if _, updateErr := metadata.UpsertTag(ctx, *tag); updateErr != nil {
		t.Fatalf("expire tag: %v", updateErr)
	}
	manifest := requireManifestForExpiration(ctx, t, metadata, alias, repo, tag.Digest)
	manifest.ExpiresAt = expiresAt
	if _, updateErr := metadata.UpsertManifest(ctx, *manifest); updateErr != nil {
		t.Fatalf("expire manifest: %v", updateErr)
	}
}

func requireTagForExpiration(ctx context.Context, t *testing.T, metadata meta.Store, alias, repo, reference string) *meta.TagRecord {
	t.Helper()
	tag, ok, err := metadata.Tag(ctx, meta.TagKey{Alias: alias, Repository: repo, Reference: reference})
	if err != nil || !ok {
		t.Fatalf("lookup tag for expiration: ok=%v err=%v", ok, err)
	}
	return tag
}

func requireManifestForExpiration(ctx context.Context, t *testing.T, metadata meta.Store, alias, repo, digest string) *meta.ManifestRecord {
	t.Helper()
	manifest, ok, err := metadata.Manifest(ctx, meta.ManifestKey{Alias: alias, Repository: repo, Digest: digest})
	if err != nil || !ok {
		t.Fatalf("lookup manifest for expiration: ok=%v err=%v", ok, err)
	}
	return manifest
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
	defer closeTestBody(t, reader)
	body, err := io.ReadAll(reader)
	requireNoError(t, "read stored object", err)
	if string(body) != wantBody {
		t.Fatalf("stored object body = %q, want %q", string(body), wantBody)
	}
	if info == nil || info.Size != int64(len(wantBody)) {
		t.Fatalf("stored object size = %v, want %d", info, len(wantBody))
	}
}

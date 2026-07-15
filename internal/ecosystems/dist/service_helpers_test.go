package dist_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/ecosystems/dist"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func newTestService(ctx context.Context, t *testing.T, upstreamURL string, allow []string) (*dist.Service, meta.Store, object.Store) {
	t.Helper()
	return newTestServiceWithMirrors(ctx, t, upstreamURL, nil, allow)
}

func newTestServiceWithMirrors(ctx context.Context, t *testing.T, upstreamURL string, mirrors, allow []string) (*dist.Service, meta.Store, object.Store) {
	t.Helper()
	db, err := meta.OpenSQLiteWithOptions(ctx, meta.DBOptions{Path: filepath.Join(t.TempDir(), "regimux.db")})
	requireNoError(t, "open metadata", err)
	t.Cleanup(func() {
		requireNoError(t, "close metadata", db.Close())
	})
	objects, err := object.NewLocal(t.TempDir())
	requireNoError(t, "open objects", err)
	return dist.NewService(dist.ServiceDependencies{
		Config: config.Config{
			Dist: config.DistEcosystemConfig{
				"gradle": {
					Registry: upstreamURL,
					Mirrors:  mirrors,
					Allow:    allow,
				},
			},
		},
		Metadata: db,
		Objects:  objects,
	}), db, objects
}

func readResponse(t *testing.T, resp *dist.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		t.Fatalf("response body is empty")
	}
	defer closeBody(t, resp.Body)
	body, err := io.ReadAll(resp.Body)
	requireNoError(t, "read body", err)
	return string(body)
}

func writeBody(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set(distribution.HeaderContentLength, strconv.Itoa(len(body)))
	if _, err := strings.NewReader(body).WriteTo(w); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func closeBody(t *testing.T, body io.Closer) {
	t.Helper()
	if body == nil {
		return
	}
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

func assertStoredArtifact(ctx context.Context, t *testing.T, metadata meta.Store, objects object.Store, key meta.TagKey, wantBody, wantMediaType string) {
	t.Helper()
	tag, ok, err := metadata.Tag(ctx, key)
	requireNoError(t, "lookup stored tag", err)
	if !ok {
		t.Fatalf("tag %s was not stored", key.String())
	}
	manifest, ok, err := metadata.Manifest(ctx, meta.ManifestKey{Alias: key.Alias, Repository: key.Repository, Digest: tag.Digest})
	requireNoError(t, "lookup stored manifest", err)
	if !ok {
		t.Fatalf("manifest for %s was not stored", key.String())
	}
	if manifest.MediaType != wantMediaType {
		t.Fatalf("manifest media type = %q, want %q", manifest.MediaType, wantMediaType)
	}
	if manifest.Size != int64(len(wantBody)) {
		t.Fatalf("manifest size = %d, want %d", manifest.Size, len(wantBody))
	}
	blob, ok, err := metadata.Blob(ctx, meta.BlobKey{Digest: tag.Digest})
	requireNoError(t, "lookup stored blob", err)
	if !ok || blob.Size != int64(len(wantBody)) {
		t.Fatalf("blob = %#v ok=%v", blob, ok)
	}
	repoBlob, ok, err := metadata.RepoBlob(ctx, meta.RepoBlobKey{Alias: key.Alias, Repository: key.Repository, Digest: tag.Digest})
	requireNoError(t, "lookup stored repo blob", err)
	if !ok || repoBlob.SourceManifest != tag.Digest {
		t.Fatalf("repo blob = %#v ok=%v", repoBlob, ok)
	}
	reader, _, err := objects.Get(ctx, tag.Digest, object.GetOptions{})
	requireNoError(t, "open stored object", err)
	defer closeBody(t, reader)
	body, err := io.ReadAll(reader)
	requireNoError(t, "read stored object", err)
	if string(body) != wantBody {
		t.Fatalf("stored body = %q, want %q", string(body), wantBody)
	}
}

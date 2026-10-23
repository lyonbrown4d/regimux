package artifactcache_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/internal/transfer"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
)

func TestPutReturnsValidatedBodyWithoutReadingStoredObject(t *testing.T) {
	ctx := t.Context()
	tempDir := isolatedArtifactTempDir(t)
	metadata, objects := newPutLocalBodyStores(t)
	objects.getErr = errors.New("synthetic remote read failure")
	store := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})

	entry, err := store.Put(ctx, artifactcache.PutRequest{
		Key:         artifactcache.Key{Alias: "npm", Repository: "demo", Reference: "latest"},
		Body:        strings.NewReader("validated package metadata"),
		ContentType: "application/json",
		Headers:     http.Header{"ETag": []string{`"metadata-v1"`}},
	})
	require.NoError(t, err)

	body, err := io.ReadAll(entry.Body)
	require.NoError(t, err)
	require.Equal(t, "validated package metadata", string(body))
	require.Equal(t, int64(len(body)), entry.Size)
	require.Equal(t, strconv.Itoa(len(body)), entry.Headers.Get("Content-Length"))
	require.Equal(t, `"metadata-v1"`, entry.Headers.Get("ETag"))
	require.Equal(t, ocidigest.FromBytes(body).String(), entry.Digest)
	require.Zero(t, objects.gets, "Put must return its validated local body without a remote Get")
	require.Len(t, artifactTempFiles(t, tempDir), 1, "Put transfers the open spool to the returned body")
	require.NoError(t, entry.Body.Close())
	require.Empty(t, artifactTempFiles(t, tempDir), "closing the returned body must remove its spool")
}

func TestPutReturnedBodySupportsRangeReads(t *testing.T) {
	metadata, objects := newPutLocalBodyStores(t)
	objects.getErr = errors.New("remote Get must not be needed")
	store := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	entry, err := store.Put(t.Context(), artifactcache.PutRequest{
		Key:  artifactcache.Key{Alias: "maven", Repository: "demo", Reference: "demo.jar"},
		Body: strings.NewReader("0123456789"),
	})
	require.NoError(t, err)

	response, err := transfer.ApplyRange(http.MethodGet, "bytes=3-6", "", transfer.Response{
		Status: http.StatusOK, Headers: entry.Headers, Body: entry.Body, Size: entry.Size,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, response.Status)
	require.Equal(t, "bytes 3-6/10", response.Headers.Get("Content-Range"))
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "3456", string(body))
	require.NoError(t, response.Body.Close())
	require.Zero(t, objects.gets)
}

func TestPutObjectWriteFailureRemovesTempFile(t *testing.T) {
	metadata, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "meta.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metadata.Close()) })
	objects := &putFailureObjectStore{putErr: errors.New("synthetic object write failure")}
	store := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	tempDir := isolatedArtifactTempDir(t)
	before := artifactTempFiles(t, tempDir)

	_, err = store.Put(t.Context(), artifactcache.PutRequest{
		Key:  artifactcache.Key{Alias: "npm", Repository: "demo", Reference: "latest"},
		Body: strings.NewReader("validated package metadata"),
	})
	require.ErrorIs(t, err, objects.putErr)
	require.Equal(t, before, artifactTempFiles(t, tempDir), "failed Put must remove its temporary body file")
}

func TestPutMetadataFailureRemovesTempFile(t *testing.T) {
	tempDir := isolatedArtifactTempDir(t)
	metadata, objects := newPutLocalBodyStores(t)
	metadataErr := errors.New("synthetic manifest metadata failure")
	store := artifactcache.New(artifactcache.Dependencies{
		Metadata: manifestFailureMetadata{MetadataStore: metadata, err: metadataErr},
		Objects:  objects,
	})
	before := artifactTempFiles(t, tempDir)

	_, err := store.Put(t.Context(), artifactcache.PutRequest{
		Key:  artifactcache.Key{Alias: "npm", Repository: "demo", Reference: "latest"},
		Body: strings.NewReader("validated package metadata"),
	})
	require.ErrorIs(t, err, metadataErr)
	require.Equal(t, before, artifactTempFiles(t, tempDir), "metadata failure must close and remove the spool")
}

func TestPutValidatorFailureSkipsObjectWriteAndRemovesTempFile(t *testing.T) {
	tempDir := isolatedArtifactTempDir(t)
	metadata, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "meta.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metadata.Close()) })
	objects := &putFailureObjectStore{putErr: errors.New("object Put must not run for invalid content")}
	store := artifactcache.New(artifactcache.Dependencies{Metadata: metadata, Objects: objects})
	validationErr := errors.New("synthetic invalid artifact")
	before := artifactTempFiles(t, tempDir)

	_, err = store.Put(t.Context(), artifactcache.PutRequest{
		Key:       artifactcache.Key{Alias: "npm", Repository: "demo", Reference: "latest"},
		Body:      strings.NewReader("invalid artifact"),
		Validator: func(io.ReaderAt, int64) error { return validationErr },
	})
	require.ErrorIs(t, err, validationErr)
	require.Zero(t, objects.puts, "validation failure must happen before object Put")
	require.Equal(t, before, artifactTempFiles(t, tempDir), "validation failure must remove the spool")
}

func newPutLocalBodyStores(t *testing.T) (*meta.SQLStore, *getFailureObjectStore) {
	t.Helper()
	metadata, err := meta.OpenSQLiteWithOptions(t.Context(), meta.DBOptions{Path: filepath.Join(t.TempDir(), "meta.db")})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, metadata.Close()) })
	local, err := object.NewLocal(t.TempDir())
	require.NoError(t, err)
	wrapped := &getFailureObjectStore{Store: local}
	return metadata, wrapped
}

type getFailureObjectStore struct {
	object.Store
	getErr error
	gets   int
}

func (s *getFailureObjectStore) Get(context.Context, string, object.GetOptions) (io.ReadCloser, *object.Info, error) {
	s.gets++
	return nil, nil, s.getErr
}

type putFailureObjectStore struct {
	object.Store
	putErr error
	puts   int
}

func (s *putFailureObjectStore) Put(context.Context, string, io.Reader, object.PutOptions) (*object.Info, error) {
	s.puts++
	return nil, s.putErr
}

type manifestFailureMetadata struct {
	artifactcache.MetadataStore
	err error
}

func (s manifestFailureMetadata) UpsertManifest(context.Context, meta.ManifestRecord) (*meta.ManifestRecord, error) {
	return nil, s.err
}

func isolatedArtifactTempDir(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)
	return tempDir
}

func artifactTempFiles(t *testing.T, tempDir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(tempDir, "regimux-artifact-cache-*"))
	require.NoError(t, err)
	return files
}

func TestPutRejectsStoredObjectSizeMismatch(t *testing.T) {
	metadata, objects := newPutLocalBodyStores(t)
	store := artifactcache.New(artifactcache.Dependencies{
		Metadata: metadata,
		Objects:  &putSizeMismatchStore{Store: objects.Store},
	})
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)
	key := artifactcache.Key{Alias: "npm", Repository: "demo", Reference: "metadata"}
	entry, err := store.Put(t.Context(), artifactcache.PutRequest{
		Key: key, Body: strings.NewReader("verified metadata"),
	})
	if entry.Body != nil {
		t.Cleanup(func() { require.NoError(t, entry.Body.Close()) })
	}
	require.ErrorContains(t, err, "size")
	_, found, lookupErr := metadata.Tag(t.Context(), meta.TagKey{
		Alias: key.Alias, Repository: key.Repository, Reference: key.Reference,
	})
	require.NoError(t, lookupErr)
	require.False(t, found, "an inconsistent object must not publish cache metadata")
	require.Empty(t, artifactTempFiles(t, tempDir))
}

type putSizeMismatchStore struct {
	object.Store
}

func (s *putSizeMismatchStore) Put(ctx context.Context, digest string, body io.Reader, opts object.PutOptions) (*object.Info, error) {
	info, err := s.Store.Put(ctx, digest, body, opts)
	if err != nil {
		return nil, fmt.Errorf("write mismatched-size fixture object: %w", err)
	}
	info.Size++
	return info, nil
}

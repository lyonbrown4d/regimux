package object_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/reference"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestLocalStorePutCreatesCASObject(t *testing.T) {
	ctx := context.Background()
	store, root := newLocalStore(t)
	body := []byte("registry object body")

	digest, info := putTestObject(ctx, t, store, body)
	if info.Digest != digest || info.Size != int64(len(body)) {
		t.Fatalf("unexpected info: %#v", info)
	}
	if _, err := os.Stat(expectedCASPath(root, digest)); err != nil {
		t.Fatalf("expected CAS object: %v", err)
	}
}

func TestLocalStoreGetReadsObject(t *testing.T) {
	ctx := context.Background()
	store, _ := newLocalStore(t)
	body := []byte("registry object body")
	digest, _ := putTestObject(ctx, t, store, body)

	reader, got, err := store.Get(ctx, digest, object.GetOptions{})
	requireNoError(t, "get", err)
	data := readAllAndClose(t, reader)
	if !bytes.Equal(data, body) || got.Size != int64(len(body)) {
		t.Fatalf("unexpected read: body=%q info=%#v", data, got)
	}
}

func TestLocalStoreGetRangeReadsPartialObject(t *testing.T) {
	ctx := context.Background()
	store, _ := newLocalStore(t)
	body := []byte("registry object body")
	digest, _ := putTestObject(ctx, t, store, body)

	ranged, info, err := store.Get(ctx, digest, object.GetOptions{
		Range: &reference.HTTPRange{Start: 9, End: 14},
	})
	requireNoError(t, "range get", err)
	data := readAllAndClose(t, ranged)
	if string(data) != "object" || info.Size != 6 {
		t.Fatalf("unexpected range read: body=%q info=%#v", data, info)
	}
}

func TestLocalStoreDeleteRemovesObject(t *testing.T) {
	ctx := context.Background()
	store, _ := newLocalStore(t)
	digest, _ := putTestObject(ctx, t, store, []byte("registry object body"))

	err := store.Delete(ctx, digest)
	requireNoError(t, "delete", err)
	ok, err := store.Exists(ctx, digest)
	requireNoError(t, "exists after delete", err)
	if ok {
		t.Fatal("expected object to be deleted")
	}
}

func TestLocalStoreRejectsDigestMismatch(t *testing.T) {
	ctx := context.Background()
	store, _ := newLocalStore(t)
	errDigest := digestFor([]byte("expected"))

	_, err := store.Put(ctx, errDigest, bytes.NewReader([]byte("actual")), object.PutOptions{})
	if !errors.Is(err, object.ErrDigestMismatch) {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
	ok, existsErr := store.Exists(ctx, errDigest)
	requireNoError(t, "exists", existsErr)
	if ok {
		t.Fatal("mismatched object must not be committed")
	}
}

func newLocalStore(t *testing.T) (*object.LocalStore, string) {
	t.Helper()
	root := t.TempDir()
	store, err := object.NewLocal(root)
	requireNoError(t, "new store", err)
	return store, root
}

func putTestObject(ctx context.Context, t *testing.T, store *object.LocalStore, body []byte) (string, *object.Info) {
	t.Helper()
	digest := digestFor(body)
	info, err := store.Put(ctx, digest, bytes.NewReader(body), object.PutOptions{
		ContentType: "application/octet-stream",
	})
	requireNoError(t, "put", err)

	ok, err := store.Exists(ctx, digest)
	requireNoError(t, "exists", err)
	if !ok {
		t.Fatal("expected object to exist")
	}
	return digest, info
}

func readAllAndClose(t *testing.T, reader io.ReadCloser) []byte {
	t.Helper()
	data, err := io.ReadAll(reader)
	closeErr := reader.Close()
	requireNoError(t, "close reader", closeErr)
	requireNoError(t, "read", err)
	return data
}

func expectedCASPath(root, digest string) string {
	encoded := digest[len("sha256:"):]
	return filepath.Join(root, "blobs", "sha256", encoded[:2], encoded)
}

func digestFor(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

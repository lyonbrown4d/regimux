package object_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/store/object"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	ocidigest "github.com/opencontainers/go-digest"
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

func TestLocalStorePersistsAfterReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	body := []byte("registry object body")

	store, err := object.NewLocal(root)
	requireNoError(t, "new store", err)
	digest, _ := putTestObject(ctx, t, store, body)

	reopened, err := object.NewLocal(root)
	requireNoError(t, "reopen store", err)
	reader, got, err := reopened.Get(ctx, digest, object.GetOptions{})
	requireNoError(t, "get reopened", err)
	data := readAllAndClose(t, reader)
	if !bytes.Equal(data, body) || got.Size != int64(len(body)) {
		t.Fatalf("unexpected reopened read: body=%q info=%#v", data, got)
	}
}

func TestLocalStoreGetRangeReadsPartialObject(t *testing.T) {
	ctx := context.Background()
	store, _ := newLocalStore(t)
	body := []byte("registry object body")
	digest, _ := putTestObject(ctx, t, store, body)

	ranged, info, err := store.Get(ctx, digest, object.GetOptions{
		Range: &object.HTTPRange{Start: 9, End: 14},
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

func TestLocalStoreWalkObjectsListsCASObjects(t *testing.T) {
	ctx := context.Background()
	store, _ := newLocalStore(t)
	firstDigest, firstInfo := putTestObject(ctx, t, store, []byte("first registry object body"))
	secondDigest, secondInfo := putTestObject(ctx, t, store, []byte("second registry object body"))

	got := walkObjectDigests(ctx, t, store)
	want := []string{firstDigest, secondDigest}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("walked digests = %v, want %v", got, want)
	}

	listed, err := store.ListObjects(ctx)
	requireNoError(t, "list objects", err)
	if len(listed) != 2 || totalObjectBytes(listed) != firstInfo.Size+secondInfo.Size {
		t.Fatalf("unexpected listed objects: %#v", listed)
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

func TestMemoryStorePutGetDelete(t *testing.T) {
	ctx := context.Background()
	store, err := object.NewMemory("memory-objects")
	requireNoError(t, "new memory store", err)
	body := []byte("registry memory object body")
	digest, info := putTestObject(ctx, t, store, body)

	reader, got, err := store.Get(ctx, digest, object.GetOptions{})
	requireNoError(t, "memory get", err)
	data := readAllAndClose(t, reader)
	if !bytes.Equal(data, body) || got.Size != info.Size {
		t.Fatalf("unexpected memory read: body=%q info=%#v", data, got)
	}

	err = store.Delete(ctx, digest)
	requireNoError(t, "memory delete", err)
	ok, err := store.Exists(ctx, digest)
	requireNoError(t, "memory exists after delete", err)
	if ok {
		t.Fatal("expected memory object to be deleted")
	}
}

func TestMemoryStoreWalkObjectsListsCASObjects(t *testing.T) {
	ctx := context.Background()
	store, err := object.NewMemory("memory-objects")
	requireNoError(t, "new memory store", err)
	digest, _ := putTestObject(ctx, t, store, []byte("registry memory object body"))

	got := walkObjectDigests(ctx, t, store)
	if len(got) != 1 || got[0] != digest {
		t.Fatalf("walked digests = %v, want [%s]", got, digest)
	}
}

func newLocalStore(t *testing.T) (*object.LocalStore, string) {
	t.Helper()
	root := t.TempDir()
	store, err := object.NewLocal(root)
	requireNoError(t, "new store", err)
	return store, root
}

func putTestObject(ctx context.Context, t *testing.T, store object.Store, body []byte) (string, *object.Info) {
	t.Helper()
	digest := digestFor(body)
	info, err := store.Put(ctx, digest, bytes.NewReader(body), object.PutOptions{
		ContentType: distribution.MediaTypeOctetStream,
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

func walkObjectDigests(ctx context.Context, t *testing.T, store object.ObjectWalker) []string {
	t.Helper()
	got := make([]string, 0)
	err := store.WalkObjects(ctx, func(info object.Info) error {
		got = append(got, info.Digest)
		return nil
	})
	requireNoError(t, "walk objects", err)
	return got
}

func totalObjectBytes(objects []object.Info) int64 {
	var total int64
	for _, info := range objects {
		total += info.Size
	}
	return total
}

func expectedCASPath(root, digest string) string {
	algorithm := ocidigest.SHA256.String()
	encoded := digest[len(algorithm)+1:]
	return filepath.Join(root, "blobs", algorithm, encoded[:2], encoded)
}

func digestFor(body []byte) string {
	return ocidigest.SHA256.FromBytes(body).String()
}

func requireNoError(t *testing.T, action string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

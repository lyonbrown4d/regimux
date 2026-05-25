package object

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
)

func TestLocalStorePutGetExistsDelete(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewLocal(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	body := []byte("registry object body")
	digest := digestFor(body)
	info, err := store.Put(ctx, digest, bytes.NewReader(body), PutOptions{ContentType: "application/octet-stream"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if info.Digest != digest || info.Size != int64(len(body)) {
		t.Fatalf("unexpected info: %#v", info)
	}

	ok, err := store.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !ok {
		t.Fatal("expected object to exist")
	}

	expectedPath := filepath.Join(root, "blobs", "sha256", digest[len("sha256:"):len("sha256:")+2], digest[len("sha256:"):])
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected CAS path %s: %v", expectedPath, err)
	}

	reader, got, err := store.Get(ctx, digest, GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close reader: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(data, body) || got.Size != int64(len(body)) {
		t.Fatalf("unexpected read: body=%q info=%#v", data, got)
	}

	ranged, rangedInfo, err := store.Get(ctx, digest, GetOptions{Range: &reference.HTTPRange{Start: 9, End: 14}})
	if err != nil {
		t.Fatalf("range get: %v", err)
	}
	rangeData, err := io.ReadAll(ranged)
	if closeErr := ranged.Close(); closeErr != nil {
		t.Fatalf("close range reader: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("range read: %v", err)
	}
	if string(rangeData) != "object" || rangedInfo.Size != 6 {
		t.Fatalf("unexpected range read: body=%q info=%#v", rangeData, rangedInfo)
	}

	if err := store.Delete(ctx, digest); err != nil {
		t.Fatalf("delete: %v", err)
	}
	ok, err = store.Exists(ctx, digest)
	if err != nil {
		t.Fatalf("exists after delete: %v", err)
	}
	if ok {
		t.Fatal("expected object to be deleted")
	}
}

func TestLocalStoreRejectsDigestMismatch(t *testing.T) {
	ctx := context.Background()
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	errDigest := digestFor([]byte("expected"))
	_, err = store.Put(ctx, errDigest, bytes.NewReader([]byte("actual")), PutOptions{})
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
	ok, existsErr := store.Exists(ctx, errDigest)
	if existsErr != nil {
		t.Fatalf("exists: %v", existsErr)
	}
	if ok {
		t.Fatal("mismatched object must not be committed")
	}
}

func digestFor(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

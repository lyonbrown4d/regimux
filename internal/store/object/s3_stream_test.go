package object_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
)

func TestS3SeekableUploadPreservesCurrentOffset(t *testing.T) {
	client := newFakeS3Client()
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "offset-test", Prefix: "offset"})
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "ignore this prefix:"
	const payload = "verified suffix"
	reader := bytes.NewReader([]byte(prefix + payload))
	if _, seekErr := reader.Seek(int64(len(prefix)), io.SeekStart); seekErr != nil {
		t.Fatal(seekErr)
	}
	digest := ocidigest.FromString(payload).String()
	info, err := store.Put(t.Context(), digest, reader, object.PutOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := client.object(testS3Key("offset", digest))
	if !ok || string(stored.body) != payload || info.Size != int64(len(payload)) {
		t.Fatalf("offset upload content=%q found=%v info=%+v", stored.body, ok, info)
	}
}

func TestS3MultipartStreamsFileSectionsWithBoundedHeap(t *testing.T) {
	const uploads = 2
	const partSize = 5 << 20
	const size = 2*partSize + 1024
	client := &blockedPartMemoryS3Client{blockedMemoryS3Client: newBlockedMemoryS3Client(4)}
	store, err := object.NewS3WithClient(client, object.S3Options{
		Bucket: "multipart-memory", PartSize: partSize, UploadConcurrency: 2, MaxConcurrentUploads: uploads,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	defer client.unblock()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	results := startStreamingPartUploads(ctx, store, uploads, size)
	waitForMemoryUploadEntries(ctx, t, client.entered, 4)
	runtime.GC()
	var during runtime.MemStats
	runtime.ReadMemStats(&during)
	delta := liveHeapGrowth(before.HeapAlloc, during.HeapAlloc)
	t.Logf("blocked multipart streams: bytes=%d live_heap_growth=%d before=%d after=%d", uploads*size, delta, before.HeapAlloc, during.HeapAlloc)
	client.unblock()
	waitForMemoryUploadResults(ctx, t, results, uploads)
	if delta > 8<<20 {
		t.Fatalf("file-backed multipart streams retained %d bytes, want less than 8 MiB", delta)
	}
	if count := client.uploadCount(); count != 0 {
		t.Fatalf("unfinished multipart uploads = %d", count)
	}
}

func startStreamingPartUploads(ctx context.Context, store *object.S3Store, count int, size int64) <-chan error {
	results := make(chan error, count)
	for index := range count {
		go func() {
			seed := repeatedS3Bytes{value: byte((index + 1) & 0xff)}
			results <- uploadStreamingPart(ctx, store, seed, size)
		}()
	}
	return results
}

func uploadStreamingPart(ctx context.Context, store *object.S3Store, seed repeatedS3Bytes, size int64) error {
	digester := ocidigest.Canonical.Digester()
	if _, err := io.Copy(digester.Hash(), io.LimitReader(seed, size)); err != nil {
		return fmt.Errorf("hash synthetic upload: %w", err)
	}
	info, err := store.Put(ctx, digester.Digest().String(), io.LimitReader(seed, size), object.PutOptions{})
	if err != nil {
		return fmt.Errorf("stream synthetic upload: %w", err)
	}
	if info.Size != size {
		return fmt.Errorf("multipart size = %d, want %d", info.Size, size)
	}
	return verifyMemoryS3Object(ctx, store, digester.Digest().String(), size)
}

type repeatedS3Bytes struct{ value byte }

func (reader repeatedS3Bytes) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = reader.value
	}
	return len(buffer), nil
}

type blockedPartMemoryS3Client struct{ *blockedMemoryS3Client }

func (client *blockedPartMemoryS3Client) UploadPart(
	ctx context.Context,
	input *s3.UploadPartInput,
	opts ...func(*s3.Options),
) (*s3.UploadPartOutput, error) {
	if err := client.wait(ctx); err != nil {
		return nil, err
	}
	return client.fakeS3Client.UploadPart(ctx, input, opts...)
}

func TestS3SpoolIsRemovedAfterDigestMismatch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	client := newFakeS3Client()
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "spool-test"})
	if err != nil {
		t.Fatal(err)
	}
	body := io.LimitReader(repeatedS3Bytes{value: 1}, 128<<10)
	_, err = store.Put(t.Context(), ocidigest.FromString("wrong digest").String(), body, object.PutOptions{})
	if !errors.Is(err, object.ErrDigestMismatch) {
		t.Fatalf("digest mismatch = %v, want ErrDigestMismatch", err)
	}
	entries, readErr := os.ReadDir(tmp)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 || client.uploadCount() != 0 {
		t.Fatalf("failed upload retained files=%d multipart uploads=%d", len(entries), client.uploadCount())
	}
}

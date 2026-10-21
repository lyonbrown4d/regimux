package object_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/lyonbrown4d/regimux/internal/store/object"
	ocidigest "github.com/opencontainers/go-digest"
)

func TestS3SmallConcurrentUploadsKeepBoundedLiveBuffers(t *testing.T) {
	const uploads = 4
	client := newBlockedMemoryS3Client(uploads)
	store, err := object.NewS3WithClient(client, object.S3Options{Bucket: "memory-test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	defer client.unblock()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	results := startSmallMemoryUploads(ctx, store, uploads)
	waitForMemoryUploadEntries(ctx, t, client.entered, uploads)
	runtime.GC()
	var during runtime.MemStats
	runtime.ReadMemStats(&during)
	delta := liveHeapGrowth(before.HeapAlloc, during.HeapAlloc)
	t.Logf("blocked small uploads: payload=%d bytes live_heap_growth=%d bytes before=%d after=%d", uploads*1024, delta, before.HeapAlloc, during.HeapAlloc)
	client.unblock()
	waitForMemoryUploadResults(ctx, t, results, uploads)
	// Leave ample room for runtime noise while rejecting four retained 16 MiB arrays.
	if delta > 8<<20 {
		t.Fatalf("four 1 KiB uploads retained %d heap bytes, want less than 8 MiB", delta)
	}
}

func TestS3UploadAdmissionCancelsWithoutReadingAndRecovers(t *testing.T) {
	client := newBlockedMemoryS3Client(2)
	store, err := object.NewS3WithClient(client, object.S3Options{
		Bucket: "admission-test", MaxConcurrentUploads: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	defer client.unblock()
	results := startSmallMemoryUploads(ctx, store, 2)
	waitForMemoryUploadEntries(ctx, t, client.entered, 2)
	payload := bytes.Repeat([]byte{3}, 1024)
	reader := &countedS3Reader{Reader: bytes.NewReader(payload)}
	waitCtx, cancelWait := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelWait()
	_, err = store.Put(waitCtx, ocidigest.FromBytes(payload).String(), reader, object.PutOptions{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting upload error = %v, want context deadline", err)
	}
	if got := reader.reads.Load(); got != 0 {
		t.Fatalf("waiting upload consumed its source %d times before admission", got)
	}
	client.unblock()
	waitForMemoryUploadResults(ctx, t, results, 2)
	if _, err := store.Put(ctx, ocidigest.FromBytes(payload).String(), bytes.NewReader(payload), object.PutOptions{}); err != nil {
		t.Fatalf("upload after cancellation and release: %v", err)
	}
}

type countedS3Reader struct {
	io.Reader
	reads atomic.Int32
}

func (reader *countedS3Reader) Read(buffer []byte) (int, error) {
	reader.reads.Add(1)
	n, err := reader.Reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read counted S3 upload source: %w", err)
	}
	return n, nil
}

func liveHeapGrowth(before, during uint64) uint64 {
	if during <= before {
		return 0
	}
	return during - before
}

func startSmallMemoryUploads(ctx context.Context, store *object.S3Store, count int) <-chan error {
	results := make(chan error, count)
	for index := range count {
		go func() {
			payload := bytes.Repeat([]byte{byte((index + 1) & 0xff)}, 1024)
			digest := ocidigest.FromBytes(payload).String()
			// Hide seeking to exercise unknown-length streaming HTTP bodies too.
			reader := struct{ io.Reader }{Reader: bytes.NewReader(payload)}
			info, err := store.Put(ctx, digest, reader, object.PutOptions{})
			if err == nil && (info.Size != int64(len(payload)) || info.Digest != digest) {
				err = fmt.Errorf("unexpected stored object: %+v", info)
			}
			if err == nil {
				err = verifyMemoryS3Object(ctx, store, digest, int64(len(payload)))
			}
			results <- err
		}()
	}
	return results
}

func verifyMemoryS3Object(ctx context.Context, store *object.S3Store, digest string, size int64) error {
	body, _, err := store.Get(ctx, digest, object.GetOptions{})
	if err != nil {
		return fmt.Errorf("open uploaded object: %w", err)
	}
	digester := ocidigest.Canonical.Digester()
	written, readErr := io.Copy(digester.Hash(), body)
	if err := errors.Join(readErr, body.Close()); err != nil {
		return fmt.Errorf("read uploaded object: %w", err)
	}
	if written != size || digester.Digest().String() != digest {
		return fmt.Errorf("uploaded content mismatch: size=%d digest=%s", written, digester.Digest())
	}
	return nil
}

type blockedMemoryS3Client struct {
	*fakeS3Client
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockedMemoryS3Client(capacity int) *blockedMemoryS3Client {
	return &blockedMemoryS3Client{
		fakeS3Client: newFakeS3Client(),
		entered:      make(chan struct{}, capacity),
		release:      make(chan struct{}),
	}
}

func (client *blockedMemoryS3Client) unblock() {
	client.once.Do(func() { close(client.release) })
}

func (client *blockedMemoryS3Client) PutObject(
	ctx context.Context,
	input *s3.PutObjectInput,
	opts ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	if err := client.wait(ctx); err != nil {
		return nil, err
	}
	return client.fakeS3Client.PutObject(ctx, input, opts...)
}

func (client *blockedMemoryS3Client) wait(ctx context.Context) error {
	select {
	case client.entered <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("admit synthetic S3 upload: %w", ctx.Err())
	}
	select {
	case <-client.release:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for synthetic S3 upload: %w", ctx.Err())
	}
}

func waitForMemoryUploadEntries(ctx context.Context, t *testing.T, entered <-chan struct{}, count int) {
	t.Helper()
	for range count {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatalf("uploads did not reach blocked object store: %v", ctx.Err())
		}
	}
}

func waitForMemoryUploadResults(ctx context.Context, t *testing.T, results <-chan error, count int) {
	t.Helper()
	for range count {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatalf("uploads did not finish: %v", ctx.Err())
		}
	}
}

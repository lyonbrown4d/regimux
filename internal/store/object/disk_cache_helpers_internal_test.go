package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ocidigest "github.com/opencontainers/go-digest"
)

type diskCacheTestBackend struct {
	canonical Store
	gets      atomic.Int64
	get       func(context.Context, string, GetOptions) (io.ReadCloser, *Info, error)
}

func newDiskCacheTestBackend(t *testing.T) (*diskCacheTestBackend, string, []byte) {
	t.Helper()

	payload := []byte("disk cache content for full and range reads")
	digest := ocidigest.FromBytes(payload).String()
	local, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("create local backend: %v", err)
	}
	if _, err := local.Put(t.Context(), digest, bytes.NewReader(payload), PutOptions{ContentType: "application/octet-stream"}); err != nil {
		t.Fatalf("seed local backend: %v", err)
	}

	backend := &diskCacheTestBackend{canonical: local}
	return backend, digest, payload
}

func (b *diskCacheTestBackend) Get(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	b.gets.Add(1)
	if b.get != nil {
		return b.get(ctx, digest, opts)
	}
	return b.getCanonical(ctx, digest, opts)
}

func (b *diskCacheTestBackend) getCanonical(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	reader, info, err := b.canonical.Get(ctx, digest, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("get canonical test object: %w", err)
	}
	return reader, info, nil
}

func (b *diskCacheTestBackend) Stat(ctx context.Context, digest string) (*Info, error) {
	info, err := b.canonical.Stat(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("stat canonical test object: %w", err)
	}
	return info, nil
}

func (b *diskCacheTestBackend) Exists(ctx context.Context, digest string) (bool, error) {
	exists, err := b.canonical.Exists(ctx, digest)
	if err != nil {
		return false, fmt.Errorf("check canonical test object: %w", err)
	}
	return exists, nil
}

func (b *diskCacheTestBackend) Put(ctx context.Context, digest string, reader io.Reader, opts PutOptions) (*Info, error) {
	info, err := b.canonical.Put(ctx, digest, reader, opts)
	if err != nil {
		return nil, fmt.Errorf("put canonical test object: %w", err)
	}
	return info, nil
}

func (b *diskCacheTestBackend) Delete(ctx context.Context, digest string) error {
	if err := b.canonical.Delete(ctx, digest); err != nil {
		return fmt.Errorf("delete canonical test object: %w", err)
	}
	return nil
}

func (b *diskCacheTestBackend) WalkObjects(ctx context.Context, fn func(Info) error) error {
	walker, ok := b.canonical.(ObjectWalker)
	if !ok {
		return fmt.Errorf("test backend does not support walking: %w", errors.ErrUnsupported)
	}
	if err := walker.WalkObjects(ctx, fn); err != nil {
		return fmt.Errorf("walk canonical test objects: %w", err)
	}
	return nil
}

func (b *diskCacheTestBackend) ListObjects(ctx context.Context) ([]Info, error) {
	lister, ok := b.canonical.(ObjectLister)
	if !ok {
		return nil, fmt.Errorf("test backend does not support listing: %w", errors.ErrUnsupported)
	}
	objects, err := lister.ListObjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list canonical test objects: %w", err)
	}
	return objects, nil
}

func diskCacheTestOptions(t *testing.T) DiskCacheOptions {
	t.Helper()
	return DiskCacheOptions{
		Directory:          t.TempDir(),
		MaxBytes:           1 << 20,
		MaxObjectBytes:     1 << 19,
		MaxEntries:         8,
		MaxConcurrentFills: 2,
		TTL:                time.Hour,
	}
}

func readDiskCacheBody(t *testing.T, reader io.ReadCloser) []byte {
	t.Helper()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close object: %v", err)
	}
	return body
}

type diskCacheGateReader struct {
	ctx     context.Context
	source  io.ReadCloser
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *diskCacheGateReader) Read(buffer []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	select {
	case <-r.release:
	case <-r.ctx.Done():
		return 0, fmt.Errorf("test gate context done: %w", r.ctx.Err())
	}
	n, err := r.source.Read(buffer)
	if err != nil {
		return n, fmt.Errorf("read gated test source: %w", err)
	}
	return n, nil
}

func (r *diskCacheGateReader) Close() error {
	if err := r.source.Close(); err != nil {
		return fmt.Errorf("close gated test source: %w", err)
	}
	return nil
}

func waitForDiskCacheSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for disk cache test signal")
	}
}

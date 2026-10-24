package pypi

import (
	"context"
	"fmt"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
	"github.com/lyonbrown4d/regimux/internal/store/meta"
	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestOpenCachedPackageResetsIdleBudgetAfterReadProgress(t *testing.T) {
	synctest.Test(t, testCachedPackageProgressCycles)
}

func testCachedPackageProgressCycles(t *testing.T) {
	store := &progressingCachedPackageStore{}
	service := &Service{cache: artifactcache.New(artifactcache.Dependencies{Objects: store})}
	manifest := &meta.ManifestRecord{Digest: "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", Size: 3}
	body, err := service.openCachedPackage(t.Context(), manifest, object.GetOptions{})
	if err != nil {
		t.Fatalf("open cached package: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := body.Close(); closeErr != nil {
			t.Errorf("close cached package: %v", closeErr)
		}
	})
	assertCachedPackageProgress(t, body, store)
	if store.opens != 1 {
		t.Fatalf("object opens=%d, want no idle continuation after progress", store.opens)
	}
}

func assertCachedPackageProgress(t *testing.T, body io.Reader, store *progressingCachedPackageStore) {
	t.Helper()
	for range 3 {
		if err := readCachedPackageProgress(body, store); err != nil {
			t.Fatal(err)
		}
	}
}

func readCachedPackageProgress(body io.Reader, store *progressingCachedPackageStore) error {
	n, readErr := body.Read(make([]byte, 1))
	if n != 1 {
		return fmt.Errorf("progress read bytes=%d, want 1; opens=%d", n, store.opens)
	}
	if readErr != nil {
		return fmt.Errorf("progress read error: %w; opens=%d", readErr, store.opens)
	}
	time.Sleep(packageIdleProgressInterval)
	synctest.Wait()
	if ctxErr := store.lastContext.Err(); ctxErr != nil {
		return fmt.Errorf("cached response canceled after successful read progress: %w; opens=%d", ctxErr, store.opens)
	}
	return nil
}

const packageIdleProgressInterval = 6 * time.Second

type progressingCachedPackageStore struct {
	object.Store
	opens       int
	lastContext context.Context
}

func (s *progressingCachedPackageStore) Get(ctx context.Context, digest string, options object.GetOptions) (io.ReadCloser, *object.Info, error) {
	s.opens++
	s.lastContext = ctx
	if options.Range != nil {
		return nil, nil, fmt.Errorf("unexpected continuation range: %#v", options.Range)
	}
	return &progressingCachedPackageReader{ctx: ctx}, &object.Info{Digest: digest, Size: 3}, nil
}

type progressingCachedPackageReader struct {
	ctx    context.Context
	next   byte
	closed bool
}

func (r *progressingCachedPackageReader) Read(buffer []byte) (int, error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	time.Sleep(packageIdleProgressInterval)
	if err := r.ctx.Err(); err != nil {
		return 0, fmt.Errorf("read progressing cached package: %w", err)
	}
	if r.next >= 3 {
		return 0, io.EOF
	}
	buffer[0] = 'a' + r.next
	r.next++
	return 1, nil
}

func (r *progressingCachedPackageReader) Close() error {
	r.closed = true
	return nil
}

// Package object stores registry object blobs.
package object

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

type aferoStore struct {
	fs        afero.Fs
	root      string
	directPut bool
	close     func() error
}

type LocalStore struct {
	*aferoStore
}

type MemoryStore struct {
	*aferoStore
}

func NewLocal(root string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join("data", "objects")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, wrapError(err, "resolve object store root")
	}
	store, err := newLocalWithFS(afero.NewOsFs(), abs)
	if err != nil {
		return nil, err
	}
	return &LocalStore{aferoStore: store}, nil
}

func NewMemory(root string) (*MemoryStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join("data", "objects")
	}
	store, err := newLocalWithFS(afero.NewMemMapFs(), root)
	if err != nil {
		return nil, err
	}
	return &MemoryStore{aferoStore: store}, nil
}

func newLocalWithFS(fs afero.Fs, root string) (*aferoStore, error) {
	return newAferoStore(fs, root, false, true)
}

func newAferoStore(fs afero.Fs, root string, directPut, ensureRoot bool) (*aferoStore, error) {
	if fs == nil {
		return nil, errorf("object store filesystem is not configured")
	}
	root = filepath.Clean(root)
	base := afero.NewBasePathFs(fs, root)
	if ensureRoot {
		if err := base.MkdirAll(".", 0o750); err != nil {
			return nil, wrapError(err, "create object store root")
		}
	}
	return &aferoStore{fs: base, root: ".", directPut: directPut}, nil
}

func (s *aferoStore) Stat(ctx context.Context, digest string) (*Info, error) {
	ctx = normalizeContext(ctx)
	if err := checkContext(ctx, "stat object"); err != nil {
		return nil, err
	}
	normalized, target, err := s.path(digest)
	if err != nil {
		return nil, err
	}
	stat, err := s.fs.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, wrapError(err, "stat object %s", normalized)
	}
	return &Info{Digest: normalized, Size: stat.Size(), ETag: normalized, Path: target}, nil
}

func (s *aferoStore) Exists(ctx context.Context, digest string) (bool, error) {
	_, err := s.Stat(ctx, digest)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (s *aferoStore) Get(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
	ctx = normalizeContext(ctx)
	if err := checkContext(ctx, "get object"); err != nil {
		return nil, nil, err
	}
	info, err := s.Stat(ctx, digest)
	if err != nil {
		return nil, nil, err
	}
	file, err := s.fs.Open(info.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, wrapError(err, "open object %s", info.Digest)
	}
	if opts.Range == nil {
		return file, info, nil
	}
	resolved, err := opts.Range.Resolve(info.Size)
	if err != nil {
		return nil, nil, closeFileAfterError(file, wrapError(err, "resolve object range"))
	}
	if _, err := file.Seek(resolved.Start, io.SeekStart); err != nil {
		return nil, nil, closeFileAfterError(file, wrapError(err, "seek object range"))
	}
	ranged := *info
	ranged.Size = resolved.Length()
	return readCloser{Reader: io.LimitReader(file, resolved.Length()), closer: file}, &ranged, nil
}

func (s *aferoStore) Put(ctx context.Context, digest string, r io.Reader, opts PutOptions) (*Info, error) {
	ctx = normalizeContext(ctx)
	if err := checkContext(ctx, "put object"); err != nil {
		return nil, err
	}
	if r == nil {
		r = http.NoBody
	}

	normalized, target, err := s.path(digest)
	if err != nil {
		return nil, err
	}
	existing, found, err := s.findExisting(ctx, normalized, opts)
	if err != nil || found {
		return existing, err
	}

	if s.directPut {
		return s.putDirect(ctx, normalized, target, r, opts)
	}
	session, err := newPutSession(s, normalized, target)
	if err != nil {
		return nil, err
	}
	return session.commit(ctx, r, opts)
}

func (s *aferoStore) Delete(ctx context.Context, digest string) error {
	ctx = normalizeContext(ctx)
	if err := checkContext(ctx, "delete object"); err != nil {
		return err
	}
	_, target, err := s.path(digest)
	if err != nil {
		return err
	}
	if err := s.fs.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapError(err, "delete object")
	}
	return nil
}

func (s *aferoStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *aferoStore) findExisting(ctx context.Context, digest string, opts PutOptions) (*Info, bool, error) {
	existing, err := s.Stat(ctx, digest)
	if err == nil {
		existing.ContentType = opts.ContentType
		return existing, true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (s *aferoStore) path(digest string) (string, string, error) {
	if s == nil || s.fs == nil || s.root == "" {
		return "", "", errorf("object store is not configured")
	}
	normalized, err := normalizeDigest(digest)
	if err != nil {
		return "", "", wrapError(err, "normalize object digest")
	}
	algorithm, encoded, _ := strings.Cut(normalized, ":")
	target := pathpkg.Join(s.root, "blobs", algorithm, encoded[:2], encoded)
	if strings.HasPrefix(target, "../") || pathpkg.IsAbs(target) {
		return "", "", errorf("object digest escapes root: %s", digest)
	}
	return normalized, target, nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func checkContext(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return wrapError(err, "%s context", operation)
	}
	return nil
}

func closeFileAfterError(file afero.File, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return joinError("close object file after error", err, wrapError(closeErr, "close object file"))
	}
	return err
}

type readCloser struct {
	io.Reader
	closer io.Closer
}

func (r readCloser) Close() error {
	if err := r.closer.Close(); err != nil {
		return wrapError(err, "close object reader")
	}
	return nil
}

var (
	_ Store = (*LocalStore)(nil)
	_ Store = (*MemoryStore)(nil)
	_ Store = (*aferoStore)(nil)

	_ ObjectWalker = (*LocalStore)(nil)
	_ ObjectWalker = (*MemoryStore)(nil)
	_ ObjectLister = (*LocalStore)(nil)
	_ ObjectLister = (*MemoryStore)(nil)
)

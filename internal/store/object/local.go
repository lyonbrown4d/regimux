// Package object stores registry object blobs.
package object

import (
	"context"
	"errors"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/reference"
	ocidigest "github.com/opencontainers/go-digest"
	"github.com/spf13/afero"
)

type LocalStore struct {
	fs   afero.Fs
	root string
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
	return newLocalWithFS(afero.NewOsFs(), abs)
}

func NewMemory(root string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join("data", "objects")
	}
	return newLocalWithFS(afero.NewMemMapFs(), root)
}

func newLocalWithFS(fs afero.Fs, root string) (*LocalStore, error) {
	if fs == nil {
		return nil, errorf("object store filesystem is not configured")
	}
	root = filepath.Clean(root)
	if err := fs.MkdirAll(root, 0o750); err != nil {
		return nil, wrapError(err, "create object store root")
	}
	return &LocalStore{fs: fs, root: root}, nil
}

func (s *LocalStore) Stat(ctx context.Context, digest string) (*Info, error) {
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

func (s *LocalStore) Exists(ctx context.Context, digest string) (bool, error) {
	_, err := s.Stat(ctx, digest)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (s *LocalStore) Get(ctx context.Context, digest string, opts GetOptions) (io.ReadCloser, *Info, error) {
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

func (s *LocalStore) Put(ctx context.Context, digest string, r io.Reader, opts PutOptions) (*Info, error) {
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

	session, err := newPutSession(s, normalized, target)
	if err != nil {
		return nil, err
	}
	return session.commit(ctx, r, opts)
}

func (s *LocalStore) Delete(ctx context.Context, digest string) error {
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

func (s *LocalStore) findExisting(ctx context.Context, digest string, opts PutOptions) (*Info, bool, error) {
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

func (s *LocalStore) path(digest string) (string, string, error) {
	if s == nil || s.fs == nil || s.root == "" {
		return "", "", errorf("local object store is not configured")
	}
	normalized, err := reference.NormalizeDigest(digest)
	if err != nil {
		return "", "", wrapError(err, "normalize object digest")
	}
	algorithm, encoded, _ := strings.Cut(normalized, ":")
	target := filepath.Join(s.root, "blobs", algorithm, encoded[:2], encoded)
	rel, err := filepath.Rel(s.root, target)
	if err != nil {
		return "", "", wrapError(err, "resolve object path relative to root")
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", errorf("object digest escapes root: %s", digest)
	}
	return normalized, target, nil
}

func newDigestHash(algorithm string) (hash.Hash, error) {
	alg := ocidigest.Algorithm(algorithm)
	if !alg.Available() {
		return nil, errorf("unsupported digest hash: %s", algorithm)
	}
	return alg.Hash(), nil
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
		return errors.Join(err, wrapError(closeErr, "close object file"))
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

var _ Store = (*LocalStore)(nil)

// Package object stores registry object blobs.
package object

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/reference"
)

type LocalStore struct {
	root string
}

func NewLocal(root string) (*LocalStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join("data", "objects")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve object store root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create object store root: %w", err)
	}
	return &LocalStore{root: abs}, nil
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
	stat, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stat object %s: %w", normalized, err)
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
	file, err := os.Open(info.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("open object %s: %w", info.Digest, err)
	}
	if opts.Range == nil {
		return file, info, nil
	}
	resolved, err := opts.Range.Resolve(info.Size)
	if err != nil {
		return nil, nil, closeFileAfterError(file, fmt.Errorf("resolve object range: %w", err))
	}
	if _, err := file.Seek(resolved.Start, io.SeekStart); err != nil {
		return nil, nil, closeFileAfterError(file, fmt.Errorf("seek object range: %w", err))
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
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
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
	if s == nil || s.root == "" {
		return "", "", errors.New("local object store is not configured")
	}
	normalized, err := reference.NormalizeDigest(digest)
	if err != nil {
		return "", "", fmt.Errorf("normalize object digest: %w", err)
	}
	algorithm, encoded, _ := strings.Cut(normalized, ":")
	target := filepath.Join(s.root, "blobs", algorithm, encoded[:2], encoded)
	rel, err := filepath.Rel(s.root, target)
	if err != nil {
		return "", "", fmt.Errorf("resolve object path relative to root: %w", err)
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("object digest escapes root: %s", digest)
	}
	return normalized, target, nil
}

func newDigestHash(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "sha256":
		return sha256.New(), nil
	case "sha384":
		return sha512.New384(), nil
	case "sha512":
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unsupported digest hash: %s", algorithm)
	}
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func checkContext(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s context: %w", operation, err)
	}
	return nil
}

func closeFileAfterError(file *os.File, err error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("close object file: %w", closeErr))
	}
	return err
}

type readCloser struct {
	io.Reader
	closer io.Closer
}

func (r readCloser) Close() error {
	if err := r.closer.Close(); err != nil {
		return fmt.Errorf("close object reader: %w", err)
	}
	return nil
}

var _ Store = (*LocalStore)(nil)

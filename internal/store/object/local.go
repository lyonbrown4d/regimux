package object

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
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
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create object store root: %w", err)
	}
	return &LocalStore{root: abs}, nil
}

func (s *LocalStore) Stat(ctx context.Context, digest string) (*Info, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
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
		return nil, err
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
	if err := ctx.Err(); err != nil {
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
		return nil, nil, err
	}
	if opts.Range == nil {
		return file, info, nil
	}
	resolved, err := opts.Range.Resolve(info.Size)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if _, err := file.Seek(resolved.Start, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	ranged := *info
	ranged.Size = resolved.Length()
	return readCloser{Reader: io.LimitReader(file, resolved.Length()), closer: file}, &ranged, nil
}

func (s *LocalStore) Put(ctx context.Context, digest string, r io.Reader, opts PutOptions) (*Info, error) {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		r = http.NoBody
	}
	normalized, target, err := s.path(digest)
	if err != nil {
		return nil, err
	}
	if existing, err := s.Stat(ctx, normalized); err == nil {
		existing.ContentType = opts.ContentType
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	algorithm, expected, _ := strings.Cut(normalized, ":")
	hasher, err := newDigestHash(algorithm)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+expected+".tmp-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tmpName)
		}
	}()

	size, copyErr := io.Copy(io.MultiWriter(tmp, hasher), r)
	if copyErr != nil {
		_ = tmp.Close()
		return nil, copyErr
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != expected {
		return nil, fmt.Errorf("%w: expected %s got %s:%s", ErrDigestMismatch, normalized, algorithm, actual)
	}
	if err := os.Rename(tmpName, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return s.Stat(ctx, normalized)
		}
		if existing, statErr := s.Stat(ctx, normalized); statErr == nil {
			return existing, nil
		}
		return nil, err
	}
	keepTemp = true
	return &Info{Digest: normalized, Size: size, ContentType: opts.ContentType, ETag: normalized, Path: target}, nil
}

func (s *LocalStore) Delete(ctx context.Context, digest string) error {
	ctx = normalizeContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	_, target, err := s.path(digest)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LocalStore) path(digest string) (string, string, error) {
	if s == nil || s.root == "" {
		return "", "", errors.New("local object store is not configured")
	}
	normalized, err := reference.NormalizeDigest(digest)
	if err != nil {
		return "", "", err
	}
	algorithm, encoded, _ := strings.Cut(normalized, ":")
	target := filepath.Join(s.root, "blobs", algorithm, encoded[:2], encoded)
	rel, err := filepath.Rel(s.root, target)
	if err != nil {
		return "", "", err
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

type readCloser struct {
	io.Reader
	closer io.Closer
}

func (r readCloser) Close() error {
	return r.closer.Close()
}

var _ Store = (*LocalStore)(nil)

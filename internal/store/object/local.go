package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LocalStore stores CAS objects on the host filesystem.
type LocalStore struct {
	root string
}

func NewLocal(root string) (*LocalStore, error) {
	if root == "" {
		root = "."
	}

	return &LocalStore{root: filepath.Clean(root)}, nil
}

func (s *LocalStore) Stat(ctx context.Context, rawDigest string) (*Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("stat local object: %w", err)
	}

	normalized, objectPath, err := s.objectPath(rawDigest)
	if err != nil {
		return nil, err
	}

	fileInfo, err := os.Stat(objectPath)
	if err != nil {
		return nil, localOperationError("stat", normalized, objectPath, err)
	}
	if !fileInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("stat local object %q at %q: not a regular file", normalized, objectPath)
	}

	return &Info{
		Digest: normalized,
		Size:   fileInfo.Size(),
		ETag:   normalized,
		Path:   objectPath,
	}, nil
}

func (s *LocalStore) Exists(ctx context.Context, rawDigest string) (bool, error) {
	_, err := s.Stat(ctx, rawDigest)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}

	return false, err
}

func (s *LocalStore) Get(
	ctx context.Context,
	rawDigest string,
	opts GetOptions,
) (io.ReadCloser, *Info, error) {
	info, err := s.Stat(ctx, rawDigest)
	if err != nil {
		return nil, nil, err
	}

	file, err := os.Open(info.Path)
	if err != nil {
		return nil, nil, localOperationError("open", info.Digest, info.Path, err)
	}

	reader, resultInfo, err := localRangeReader(file, info, opts.Range)
	if err != nil {
		return nil, nil, errors.Join(err, closeLocalObject(file, info.Digest))
	}

	return &contextReadCloser{ctx: ctx, reader: reader, closer: file}, resultInfo, nil
}

func localRangeReader(
	file *os.File,
	info *Info,
	requested *HTTPRange,
) (io.Reader, *Info, error) {
	resultInfo := *info
	if requested == nil {
		return file, &resultInfo, nil
	}

	resolved, err := requested.Resolve(info.Size)
	if err != nil {
		return nil, nil, err
	}
	if _, err = file.Seek(resolved.Start, io.SeekStart); err != nil {
		return nil, nil, fmt.Errorf("seek local object %q: %w", info.Digest, err)
	}

	resultInfo.Size = resolved.Length()

	return io.LimitReader(file, resolved.Length()), &resultInfo, nil
}

func closeLocalObject(file *os.File, digest string) error {
	if err := file.Close(); err != nil {
		return fmt.Errorf("close local object %q: %w", digest, err)
	}

	return nil
}

func (s *LocalStore) Delete(ctx context.Context, rawDigest string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("delete local object: %w", err)
	}

	normalized, objectPath, err := s.objectPath(rawDigest)
	if err != nil {
		return err
	}

	err = os.Remove(objectPath)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return localOperationError("delete", normalized, objectPath, err)
}

func (s *LocalStore) objectPath(rawDigest string) (string, string, error) {
	normalized, relative, err := casRelativePath(rawDigest)
	if err != nil {
		return "", "", err
	}

	return normalized, filepath.Join(s.root, filepath.FromSlash(relative)), nil
}

func localOperationError(operation, digest, objectPath string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s local object %q: %w", operation, digest, ErrNotFound)
	}

	return fmt.Errorf("%s local object %q at %q: %w", operation, digest, objectPath, err)
}

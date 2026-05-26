package object

import (
	"context"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type putSession struct {
	store      *LocalStore
	normalized string
	target     string
	algorithm  string
	expected   string
	hasher     hash.Hash
	tmp        *os.File
	tmpName    string
	keepTemp   bool
}

func newPutSession(store *LocalStore, normalized, target string) (*putSession, error) {
	algorithm, expected, _ := strings.Cut(normalized, ":")
	hasher, err := newDigestHash(algorithm)
	if err != nil {
		return nil, err
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o750); mkdirErr != nil {
		return nil, wrapError(mkdirErr, "create object digest directory")
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+expected+".tmp-*")
	if err != nil {
		return nil, wrapError(err, "create object temp file")
	}
	return &putSession{
		store:      store,
		normalized: normalized,
		target:     target,
		algorithm:  algorithm,
		expected:   expected,
		hasher:     hasher,
		tmp:        tmp,
		tmpName:    tmp.Name(),
	}, nil
}

func (s *putSession) commit(ctx context.Context, r io.Reader, opts PutOptions) (info *Info, err error) {
	defer func() {
		if s.keepTemp {
			return
		}
		if cleanupErr := removeTempObject(s.tmpName); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	size, err := s.write(r)
	if err != nil {
		return nil, err
	}
	if err := s.validateDigest(); err != nil {
		return nil, err
	}
	return s.rename(ctx, size, opts)
}

func (s *putSession) write(r io.Reader) (int64, error) {
	size, err := io.Copy(io.MultiWriter(s.tmp, s.hasher), r)
	if err != nil {
		return 0, s.closeWithError(wrapError(err, "write object temp file"))
	}
	if err := s.tmp.Sync(); err != nil {
		return 0, s.closeWithError(wrapError(err, "sync object temp file"))
	}
	if err := s.tmp.Close(); err != nil {
		return 0, wrapError(err, "close object temp file")
	}
	return size, nil
}

func (s *putSession) validateDigest() error {
	actual := hex.EncodeToString(s.hasher.Sum(nil))
	if actual == s.expected {
		return nil
	}
	return errorf("%w: expected %s got %s:%s", ErrDigestMismatch, s.normalized, s.algorithm, actual)
}

func (s *putSession) rename(ctx context.Context, size int64, opts PutOptions) (*Info, error) {
	if err := os.Rename(s.tmpName, s.target); err != nil {
		return s.handleRenameError(ctx, err)
	}
	s.keepTemp = true
	return &Info{
		Digest:      s.normalized,
		Size:        size,
		ContentType: opts.ContentType,
		ETag:        s.normalized,
		Path:        s.target,
	}, nil
}

func (s *putSession) handleRenameError(ctx context.Context, err error) (*Info, error) {
	if errors.Is(err, os.ErrExist) {
		return s.store.Stat(ctx, s.normalized)
	}
	existing, statErr := s.store.Stat(ctx, s.normalized)
	if statErr == nil {
		return existing, nil
	}
	if errors.Is(statErr, ErrNotFound) {
		return nil, wrapError(err, "commit object file")
	}
	return nil, errors.Join(
		wrapError(err, "commit object file"),
		wrapError(statErr, "stat existing object after commit failure"),
	)
}

func (s *putSession) closeWithError(err error) error {
	if closeErr := s.tmp.Close(); closeErr != nil {
		return errors.Join(err, wrapError(closeErr, "close object temp file"))
	}
	return err
}

func removeTempObject(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapError(err, "remove object temp file")
	}
	return nil
}

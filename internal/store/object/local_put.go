package object

import (
	"context"
	"errors"
	"hash"
	"io"
	"os"
	pathpkg "path"
	"strconv"
	"strings"
	"time"

	ocidigest "github.com/opencontainers/go-digest"
	"github.com/spf13/afero"
)

type putSession struct {
	store      *aferoStore
	normalized string
	target     string
	algorithm  string
	expected   string
	hasher     hash.Hash
	tmp        afero.File
	tmpName    string
	keepTemp   bool
}

func newPutSession(store *aferoStore, normalized, target string) (*putSession, error) {
	algorithm, expected, _ := strings.Cut(normalized, ":")
	hasher, err := newDigestHash(algorithm)
	if err != nil {
		return nil, err
	}
	if mkdirErr := store.fs.MkdirAll(pathpkg.Dir(target), 0o750); mkdirErr != nil {
		return nil, wrapError(mkdirErr, "create object digest directory")
	}
	tmp, err := afero.TempFile(store.fs, pathpkg.Dir(target), "."+expected+".tmp-*")
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
		if cleanupErr := removeTempObject(s.store.fs, s.tmpName); cleanupErr != nil {
			err = joinError("cleanup object temp file after put", err, cleanupErr)
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
	actual := ocidigest.NewDigest(ocidigest.Algorithm(s.algorithm), s.hasher)
	if actual.Encoded() == s.expected {
		return nil
	}
	return NewDigestMismatch(s.normalized, actual.String())
}

func (s *putSession) rename(ctx context.Context, size int64, opts PutOptions) (*Info, error) {
	if err := s.store.fs.Rename(s.tmpName, s.target); err != nil {
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
	return nil, joinError("commit object file and stat existing object",
		wrapError(err, "commit object file"),
		wrapError(statErr, "stat existing object after commit failure"),
	)
}

func (s *putSession) closeWithError(err error) error {
	if closeErr := s.tmp.Close(); closeErr != nil {
		return joinError("close object temp file after error", err, wrapError(closeErr, "close object temp file"))
	}
	return err
}

func removeTempObject(fs afero.Fs, path string) error {
	if err := fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapError(err, "remove object temp file")
	}
	return nil
}

func (s *aferoStore) putDirect(ctx context.Context, normalized, target string, r io.Reader, opts PutOptions) (*Info, error) {
	algorithm, expected, _ := strings.Cut(normalized, ":")
	hasher, err := newDigestHash(algorithm)
	if err != nil {
		return nil, err
	}
	if mkdirErr := s.fs.MkdirAll(pathpkg.Dir(target), 0o750); mkdirErr != nil {
		return nil, wrapError(mkdirErr, "create object digest directory")
	}
	tmpName := directTempName(target, expected)
	file, err := s.fs.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return nil, wrapError(err, "create object file")
	}
	size, err := io.Copy(io.MultiWriter(file, hasher), r)
	closeErr := file.Close()
	if err != nil {
		return nil, joinError("write object file and remove temp file", wrapError(err, "write object file"), removeTempObject(s.fs, tmpName))
	}
	if closeErr != nil {
		return nil, joinError("close object file and remove temp file", wrapError(closeErr, "close object file"), removeTempObject(s.fs, tmpName))
	}

	actual := ocidigest.NewDigest(ocidigest.Algorithm(algorithm), hasher)
	if actual.Encoded() != expected {
		return nil, joinError("remove object temp file after digest mismatch",
			NewDigestMismatch(normalized, actual.String()),
			removeTempObject(s.fs, tmpName),
		)
	}
	if err := s.fs.Rename(tmpName, target); err != nil {
		return s.handleDirectCommitError(ctx, normalized, tmpName, err)
	}
	return &Info{
		Digest:      normalized,
		Size:        size,
		ContentType: opts.ContentType,
		ETag:        normalized,
		Path:        target,
	}, nil
}

func (s *aferoStore) handleDirectCommitError(ctx context.Context, normalized, tmpName string, err error) (*Info, error) {
	existing, statErr := s.Stat(ctx, normalized)
	if statErr == nil {
		return existing, removeTempObject(s.fs, tmpName)
	}
	if errors.Is(statErr, ErrNotFound) {
		return nil, joinError("commit object file and remove temp file", wrapError(err, "commit object file"), removeTempObject(s.fs, tmpName))
	}
	return nil, joinError("commit object file, stat existing object, and remove temp file",
		wrapError(err, "commit object file"),
		wrapError(statErr, "stat existing object after commit failure"),
		removeTempObject(s.fs, tmpName),
	)
}

func directTempName(target, expected string) string {
	return pathpkg.Join(pathpkg.Dir(target), "."+expected+".tmp-"+strconv.FormatInt(time.Now().UnixNano(), 36))
}

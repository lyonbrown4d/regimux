package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (s *LocalStore) Put(
	ctx context.Context,
	rawDigest string,
	reader io.Reader,
	opts PutOptions,
) (result *Info, resultErr error) {
	normalized, objectPath, err := s.objectPath(rawDigest)
	if err != nil {
		return nil, err
	}

	existing, exists, err := s.existingObject(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if exists {
		return &existing, nil
	}

	session, err := newLocalPutSession(objectPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = session.cleanup(resultErr)
	}()

	size, err := session.write(ctx, normalized, reader)
	if err != nil {
		return nil, err
	}

	existing, exists, err = session.commit(ctx, s, normalized)
	if err != nil {
		return nil, err
	}
	if exists {
		return &existing, nil
	}

	return &Info{
		Digest:      normalized,
		Size:        size,
		ContentType: opts.ContentType,
		ETag:        normalized,
		Path:        objectPath,
	}, nil
}

func (s *LocalStore) existingObject(
	ctx context.Context,
	digest string,
) (Info, bool, error) {
	info, err := s.Stat(ctx, digest)
	if err == nil {
		return *info, true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return Info{}, false, nil
	}

	return Info{}, false, err
}

type localPutSession struct {
	file      *os.File
	tempPath  string
	target    string
	closed    bool
	committed bool
}

func newLocalPutSession(target string) (*localPutSession, error) {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return nil, fmt.Errorf("create local object directory %q: %w", parent, err)
	}

	file, err := os.CreateTemp(parent, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary local object: %w", err)
	}

	return &localPutSession{
		file:     file,
		tempPath: file.Name(),
		target:   target,
	}, nil
}

func (s *localPutSession) write(
	ctx context.Context,
	digest string,
	reader io.Reader,
) (int64, error) {
	verifier := newDigestVerifier(digest)
	_, err := io.Copy(
		io.MultiWriter(s.file, verifier),
		&contextReader{ctx: ctx, reader: reader},
	)
	if err != nil {
		return 0, fmt.Errorf("write local object %q: %w", digest, err)
	}
	if err := verifier.verify(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("write local object %q: %w", digest, err)
	}
	if err := s.persist(digest); err != nil {
		return 0, err
	}

	return verifier.sizeValue(), nil
}

func (s *localPutSession) persist(digest string) error {
	if err := s.file.Chmod(0o640); err != nil {
		return fmt.Errorf("set local object permissions for %q: %w", digest, err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("sync local object %q: %w", digest, err)
	}
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close local object %q: %w", digest, err)
	}

	s.closed = true

	return nil
}

func (s *localPutSession) commit(
	ctx context.Context,
	store *LocalStore,
	digest string,
) (Info, bool, error) {
	if err := os.Rename(s.tempPath, s.target); err != nil {
		existing, exists, statErr := store.existingObject(ctx, digest)
		if exists {
			return existing, true, nil
		}

		commitErr := fmt.Errorf("commit local object %q: %w", digest, err)
		return Info{}, false, errors.Join(commitErr, statErr)
	}

	s.committed = true

	return Info{}, false, nil
}

func (s *localPutSession) cleanup(resultErr error) error {
	cleanupErr := error(nil)
	if !s.closed {
		if err := s.file.Close(); err != nil {
			cleanupErr = errors.Join(
				cleanupErr,
				fmt.Errorf("close temporary local object: %w", err),
			)
		}
		s.closed = true
	}

	if !s.committed {
		if err := os.Remove(s.tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(
				cleanupErr,
				fmt.Errorf("remove temporary local object: %w", err),
			)
		}
	}

	return errors.Join(resultErr, cleanupErr)
}

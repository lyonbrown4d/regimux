package object

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func (s *LocalStore) WalkObjects(ctx context.Context, fn func(Info) error) error {
	objectsRoot := filepath.Join(s.root, casRootDirectory)
	_, err := os.Stat(objectsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat local CAS root %q: %w", objectsRoot, err)
	}

	walkErr := filepath.WalkDir(
		objectsRoot,
		func(objectPath string, entry fs.DirEntry, entryErr error) error {
			return s.visitObject(ctx, objectPath, entry, entryErr, fn)
		},
	)
	if walkErr != nil {
		return fmt.Errorf("walk local CAS root %q: %w", objectsRoot, walkErr)
	}

	return nil
}

func (s *LocalStore) visitObject(
	ctx context.Context,
	objectPath string,
	entry fs.DirEntry,
	entryErr error,
	fn func(Info) error,
) error {
	if entryErr != nil {
		return fmt.Errorf("visit local CAS path %q: %w", objectPath, entryErr)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("walk local CAS objects: %w", err)
	}
	if entry.IsDir() || !entry.Type().IsRegular() {
		return nil
	}

	info, valid, err := s.objectInfo(objectPath, entry)
	if err != nil || !valid {
		return err
	}
	if err = fn(info); err != nil {
		return fmt.Errorf("visit local CAS object %q: %w", info.Digest, err)
	}

	return nil
}

func (s *LocalStore) objectInfo(objectPath string, entry fs.DirEntry) (Info, bool, error) {
	relative, err := filepath.Rel(s.root, objectPath)
	if err != nil {
		return Info{}, false, fmt.Errorf("resolve local CAS path %q: %w", objectPath, err)
	}

	digest, valid := casDigestFromRelativePath(filepath.ToSlash(relative))
	if !valid {
		return Info{}, false, nil
	}

	fileInfo, err := entry.Info()
	if err != nil {
		return Info{}, false, fmt.Errorf("stat local CAS object %q: %w", objectPath, err)
	}

	return Info{
		Digest: digest,
		Size:   fileInfo.Size(),
		ETag:   digest,
		Path:   objectPath,
	}, true, nil
}

func (s *LocalStore) ListObjects(ctx context.Context) ([]Info, error) {
	objects := make([]Info, 0)
	err := s.WalkObjects(ctx, func(info Info) error {
		objects = append(objects, info)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return objects, nil
}

package object

import (
	"cmp"
	"context"
	"errors"
	"hash"
	"os"
	pathpkg "path"
	"slices"
	"strings"

	ocidigest "github.com/opencontainers/go-digest"
	"github.com/samber/lo"
	"github.com/spf13/afero"
)

func (s *LocalStore) WalkObjects(ctx context.Context, fn ObjectWalkFunc) error {
	if s == nil || s.aferoStore == nil {
		return errorf("object store is not configured")
	}
	return s.walkObjects(ctx, fn)
}

func (s *LocalStore) ListObjects(ctx context.Context) ([]Info, error) {
	if s == nil || s.aferoStore == nil {
		return nil, errorf("object store is not configured")
	}
	return s.listObjects(ctx)
}

func (s *MemoryStore) WalkObjects(ctx context.Context, fn ObjectWalkFunc) error {
	if s == nil || s.aferoStore == nil {
		return errorf("object store is not configured")
	}
	return s.walkObjects(ctx, fn)
}

func (s *MemoryStore) ListObjects(ctx context.Context) ([]Info, error) {
	if s == nil || s.aferoStore == nil {
		return nil, errorf("object store is not configured")
	}
	return s.listObjects(ctx)
}

func (s *aferoStore) listObjects(ctx context.Context) ([]Info, error) {
	objects := make([]Info, 0)
	if err := s.walkObjects(ctx, func(info Info) error {
		objects = append(objects, info)
		return nil
	}); err != nil {
		return nil, err
	}
	return objects, nil
}

func (s *aferoStore) walkObjects(ctx context.Context, fn ObjectWalkFunc) error {
	ctx = normalizeContext(ctx)
	if err := checkContext(ctx, "walk objects"); err != nil {
		return err
	}
	if fn == nil {
		return errorf("object walk callback is required")
	}
	if s == nil || s.fs == nil || s.root == "" {
		return errorf("object store is not configured")
	}
	return s.walkBlobRoot(ctx, pathpkg.Join(s.root, "blobs"), fn)
}

func (s *aferoStore) walkBlobRoot(ctx context.Context, root string, fn ObjectWalkFunc) error {
	algorithms, err := readSortedDirs(s.fs, root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return wrapError(err, "list object blob algorithms")
	}
	for _, algorithm := range algorithms {
		if err := checkContext(ctx, "walk objects"); err != nil {
			return err
		}
		if _, err := newDigestHash(algorithm); err != nil {
			continue
		}
		if err := s.walkBlobAlgorithm(ctx, root, algorithm, fn); err != nil {
			return err
		}
	}
	return nil
}

func (s *aferoStore) walkBlobAlgorithm(ctx context.Context, root, algorithm string, fn ObjectWalkFunc) error {
	algorithmPath := pathpkg.Join(root, algorithm)
	prefixes, err := readSortedDirs(s.fs, algorithmPath)
	if err != nil {
		return wrapError(err, "list object blob prefixes")
	}
	for _, prefix := range prefixes {
		if err := checkContext(ctx, "walk objects"); err != nil {
			return err
		}
		if len(prefix) != 2 {
			continue
		}
		if err := s.walkBlobPrefix(ctx, algorithmPath, algorithm, prefix, fn); err != nil {
			return err
		}
	}
	return nil
}

func (s *aferoStore) walkBlobPrefix(ctx context.Context, algorithmPath, algorithm, prefix string, fn ObjectWalkFunc) error {
	prefixPath := pathpkg.Join(algorithmPath, prefix)
	files, err := afero.ReadDir(s.fs, prefixPath)
	if err != nil {
		return wrapError(err, "list object blob prefix")
	}
	slices.SortFunc(files, func(left, right os.FileInfo) int {
		return cmp.Compare(left.Name(), right.Name())
	})
	for _, file := range files {
		if err := checkContext(ctx, "walk objects"); err != nil {
			return err
		}
		info, ok := infoFromCASFile(algorithm, prefix, prefixPath, file)
		if !ok {
			continue
		}
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}

func readSortedDirs(fs afero.Fs, name string) ([]string, error) {
	entries, err := afero.ReadDir(fs, name)
	if err != nil {
		return nil, wrapError(err, "read sorted directories")
	}
	dirs := lo.FilterMap(entries, func(entry os.FileInfo, _ int) (string, bool) {
		return entry.Name(), entry.IsDir()
	})
	slices.Sort(dirs)
	return dirs, nil
}

func infoFromCASFile(algorithm, prefix, parent string, file os.FileInfo) (Info, bool) {
	if file == nil || file.IsDir() {
		return Info{}, false
	}
	encoded := file.Name()
	if strings.HasPrefix(encoded, ".") || !strings.HasPrefix(encoded, prefix) {
		return Info{}, false
	}
	digest, err := normalizeDigest(algorithm + ":" + encoded)
	if err != nil {
		return Info{}, false
	}
	return Info{
		Digest: digest,
		Size:   file.Size(),
		ETag:   digest,
		Path:   pathpkg.Join(parent, encoded),
	}, true
}

func newDigestHash(algorithm string) (hash.Hash, error) {
	alg := ocidigest.Algorithm(algorithm)
	if !alg.Available() {
		return nil, errorf("unsupported digest hash: %s", algorithm)
	}
	return alg.Hash(), nil
}

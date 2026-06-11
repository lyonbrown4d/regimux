// Package object stores registry object blobs.
package object

import (
	"context"
	"errors"
	"hash"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	ocidigest "github.com/opencontainers/go-digest"
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

func (s *LocalStore) WalkObjects(ctx context.Context, fn ObjectWalkFunc) error {
	if s == nil || s.aferoStore == nil {
		return errorf("object store is not configured")
	}
	return s.aferoStore.walkObjects(ctx, fn)
}

func (s *LocalStore) ListObjects(ctx context.Context) ([]Info, error) {
	if s == nil || s.aferoStore == nil {
		return nil, errorf("object store is not configured")
	}
	return s.aferoStore.listObjects(ctx)
}

func (s *MemoryStore) WalkObjects(ctx context.Context, fn ObjectWalkFunc) error {
	if s == nil || s.aferoStore == nil {
		return errorf("object store is not configured")
	}
	return s.aferoStore.walkObjects(ctx, fn)
}

func (s *MemoryStore) ListObjects(ctx context.Context) ([]Info, error) {
	if s == nil || s.aferoStore == nil {
		return nil, errorf("object store is not configured")
	}
	return s.aferoStore.listObjects(ctx)
}

func (s *aferoStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
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
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
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
		return nil, err
	}
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Strings(dirs)
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

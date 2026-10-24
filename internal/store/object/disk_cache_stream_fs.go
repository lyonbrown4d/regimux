package object

import (
	"errors"
	"fmt"
	"io"
	"os"

	streamlib "github.com/djherbis/stream"
)

type diskCacheStreamFS struct {
	store   *DiskCacheStore
	name    string
	created bool
}

type diskCacheStreamFile struct {
	*os.File
	name string
}

func (f *diskCacheStreamFile) Name() string { return f.name }

func (fs *diskCacheStreamFS) Create(name string) (streamlib.File, error) {
	if name != fs.name {
		return nil, errors.New("unexpected disk cache stream path")
	}
	fs.store.rootMu.RLock()
	defer fs.store.rootMu.RUnlock()
	file, err := fs.store.rootFS.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open disk cache stream writer: %w", err)
	}
	fs.created = true
	return &diskCacheStreamFile{File: file, name: name}, nil
}

func (fs *diskCacheStreamFS) Open(name string) (streamlib.File, error) {
	if name != fs.name {
		return nil, errors.New("unexpected disk cache stream path")
	}
	fs.store.rootMu.RLock()
	defer fs.store.rootMu.RUnlock()
	file, err := fs.store.rootFS.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open disk cache stream reader: %w", err)
	}
	return &diskCacheStreamFile{File: file, name: name}, nil
}

func (fs *diskCacheStreamFS) Remove(name string) error {
	if name != fs.name {
		return errors.New("unexpected disk cache stream path")
	}
	return fs.store.removeCacheFile(name)
}

var _ io.ReadCloser = (*diskCacheStreamFile)(nil)

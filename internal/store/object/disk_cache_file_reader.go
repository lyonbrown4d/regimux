package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type diskCacheFileReader struct {
	ctx      context.Context
	body     io.Reader
	file     *os.File
	store    *DiskCacheStore
	digest   string
	entry    *diskCacheEntry
	once     sync.Once
	closeErr error
}

func (r *diskCacheFileReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		closeErr := r.Close()
		return 0, errors.Join(fmt.Errorf("read cached object: %w", err), closeErr)
	}
	n, err := r.body.Read(buffer)
	if err != nil && err != io.EOF {
		closeErr := r.Close()
		return n, errors.Join(fmt.Errorf("read cached object: %w", err), closeErr)
	}
	if err == io.EOF {
		if closeErr := r.Close(); closeErr != nil {
			return n, fmt.Errorf("close cached object after read: %w", closeErr)
		}
		return n, io.EOF
	}
	return n, nil
}

func (r *diskCacheFileReader) Close() error {
	r.once.Do(func() {
		if err := r.file.Close(); err != nil {
			r.closeErr = fmt.Errorf("close cached object file: %w", err)
		}
		r.store.mu.Lock()
		delete(r.store.readers, r)
		r.store.mu.Unlock()
		r.store.releaseEntry(r.digest, r.entry)
	})
	return r.closeErr
}

var _ io.ReadCloser = (*diskCacheFileReader)(nil)

package object

import (
	"context"
	"fmt"
	"io"
	"sync"
)

type storeBoundReadCloser struct {
	reader io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
	err    error
}

func (r *storeBoundReadCloser) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	switch err {
	case io.EOF:
		return n, io.EOF
	case nil:
		return n, nil
	default:
		return n, fmt.Errorf("read canonical object: %w", err)
	}
}

func (r *storeBoundReadCloser) Close() error {
	r.once.Do(func() {
		r.cancel()
		if err := r.reader.Close(); err != nil {
			r.err = fmt.Errorf("close canonical object reader: %w", err)
		}
	})
	return r.err
}

func stopStoreCloseCallback(stop func() bool) {
	if stop != nil {
		stop()
	}
}

package object

import (
	"context"
	"fmt"
	"io"
	"sync"
)

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
	err    error
}

func (r *cancelReadCloser) Read(buffer []byte) (int, error) {
	n, err := r.ReadCloser.Read(buffer)
	if err == io.EOF {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read canonical object: %w", err)
	}
	return n, nil
}

func (r *cancelReadCloser) Close() error {
	r.once.Do(func() {
		r.cancel()
		if err := r.ReadCloser.Close(); err != nil {
			r.err = fmt.Errorf("close canonical object reader: %w", err)
		}
	})
	return r.err
}

var _ io.ReadCloser = (*cancelReadCloser)(nil)

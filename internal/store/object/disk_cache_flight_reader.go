package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

type diskCacheFlightReader struct {
	store        *DiskCacheStore
	fill         *diskCacheFill
	callerCtx    context.Context
	source       io.ReadCloser
	stopCaller   func() bool
	mu           sync.Mutex
	readBytes    int64
	closed       bool
	once         sync.Once
	closeErr     error
	directReader bool
	waitForFill  bool
}

func (r *diskCacheFlightReader) Read(buffer []byte) (int, error) {
	n, err := r.readSource(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, errors.Join(fmt.Errorf("read shared canonical object: %w", err), r.closeWithoutWaiting())
	}
	if n > 0 {
		if countErr := r.recordRead(int64(n)); countErr != nil {
			return n, errors.Join(countErr, r.closeWithoutWaiting())
		}
	}
	if errors.Is(err, io.EOF) {
		return n, r.finishReadAtEOF()
	}
	return n, nil
}

func (r *diskCacheFlightReader) readSource(buffer []byte) (int, error) {
	if err := r.callerCtx.Err(); err != nil {
		return 0, fmt.Errorf("caller context: %w", err)
	}
	n, err := r.source.Read(buffer)
	if callerErr := r.callerCtx.Err(); callerErr != nil {
		return n, fmt.Errorf("caller context: %w", callerErr)
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read shared source: %w", err)
	}
	return n, nil
}

func (r *diskCacheFlightReader) finishReadAtEOF() error {
	r.mu.Lock()
	total := r.readBytes
	r.mu.Unlock()
	if !r.directReader && total != r.fill.info.Size {
		return errors.Join(ErrDigestMismatch, r.closeWithoutWaiting())
	}
	if closeErr := r.Close(); closeErr != nil {
		return fmt.Errorf("close shared canonical reader after EOF: %w", closeErr)
	}
	return io.EOF
}

func (r *diskCacheFlightReader) recordRead(count int64) error {
	r.mu.Lock()
	r.readBytes += count
	readBytes := r.readBytes
	r.mu.Unlock()
	if r.directReader || readBytes <= r.fill.info.Size {
		return nil
	}
	return ErrDigestMismatch
}

func (r *diskCacheFlightReader) Close() error {
	return r.close(true)
}

func (r *diskCacheFlightReader) closeWithoutWaiting() error {
	return r.close(false)
}

func (r *diskCacheFlightReader) close(wait bool) error {
	r.once.Do(func() {
		r.store.mu.Lock()
		r.mu.Lock()
		r.closed = true
		fullyRead := !r.directReader && r.readBytes == r.fill.info.Size
		stopCaller := r.stopCaller
		delete(r.store.flightReaders, r)
		r.mu.Unlock()
		r.store.mu.Unlock()
		stopStoreCloseCallback(stopCaller)
		if err := r.source.Close(); err != nil {
			r.closeErr = fmt.Errorf("close shared canonical reader: %w", err)
		}
		r.waitForFill = r.store.releaseDiskCacheFlightRef(r.fill, false, fullyRead)
	})
	if wait && r.waitForFill {
		<-r.fill.done
	}
	return r.closeErr
}

var _ io.ReadCloser = (*diskCacheFlightReader)(nil)

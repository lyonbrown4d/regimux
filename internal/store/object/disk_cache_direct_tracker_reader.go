package object

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

type diskCacheDirectReader struct {
	tracker *diskCacheDirectTracker
	request *diskCacheDirectRequest
	body    io.ReadCloser
	once    sync.Once
	err     error
}

func (r *diskCacheDirectReader) Read(buffer []byte) (int, error) {
	n, err := r.body.Read(buffer)
	if err == nil {
		return n, nil
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, fmt.Errorf("read direct object body: %w", err)
}

func (r *diskCacheDirectReader) Close() error {
	r.once.Do(func() {
		r.tracker.stopRequest(r.request)
		r.err = r.tracker.closeRequestBody(r.request)
	})
	return r.err
}

func (t *diskCacheDirectTracker) closeRequestBody(req *diskCacheDirectRequest) error {
	t.mu.Lock()
	body := req.body
	if body == nil {
		closing := req.closingBody
		done := req.bodyDone
		closeErr := req.closeErr
		t.mu.Unlock()
		if closing {
			<-done
			t.mu.Lock()
			closeErr = req.closeErr
			t.mu.Unlock()
		}
		return closeErr
	}
	req.body = nil
	req.closingBody = true
	t.mu.Unlock()

	err := body.Close()
	var closeErr error
	if err != nil {
		closeErr = fmt.Errorf("close direct object body: %w", err)
	}

	t.mu.Lock()
	req.closeErr = closeErr
	if closeErr != nil && t.firstCloseErr == nil {
		t.firstCloseErr = closeErr
	}
	req.closingBody = false
	delete(t.requests, req)
	close(req.bodyDone)
	t.mu.Unlock()
	return closeErr
}

var errDiskCacheNilBody = errors.New("object store returned a nil body")

var _ io.ReadCloser = (*diskCacheDirectReader)(nil)

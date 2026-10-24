package object

import "fmt"

func (t *diskCacheDirectTracker) close() error {
	t.closeOne.Do(func() { t.closeErr = t.closeResources() })
	return t.closeErr
}

func (t *diskCacheDirectTracker) closeResources() error {
	t.mu.Lock()
	t.closed = true
	requests := make([]*diskCacheDirectRequest, 0, len(t.requests))
	for req := range t.requests {
		requests = append(requests, req)
	}
	t.mu.Unlock()

	for _, req := range requests {
		t.stopRequest(req)
		t.closeRequestBodyAndRemember(req)
	}
	t.pending.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.firstCloseErr
}

func (t *diskCacheDirectTracker) closeRequestBodyAndRemember(req *diskCacheDirectRequest) {
	if err := t.closeRequestBody(req); err != nil {
		t.rememberCloseError(req, err)
	}
}

func (t *diskCacheDirectTracker) stopRequest(req *diskCacheDirectRequest) {
	req.stopOnce.Do(func() { req.callbackStop = req.stop() })
	req.cancel()
	if !req.callbackStop {
		<-req.callbackDone
	}
}

func (t *diskCacheDirectTracker) removePendingRequest(req *diskCacheDirectRequest) {
	t.mu.Lock()
	delete(t.requests, req)
	t.mu.Unlock()
	t.stopRequest(req)
}

func (t *diskCacheDirectTracker) rememberCloseError(req *diskCacheDirectRequest, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if req.closeErr == nil {
		req.closeErr = err
	}
	if t.firstCloseErr == nil {
		t.firstCloseErr = err
	}
}

func wrapDirectOpenError(err error) error {
	return fmt.Errorf("open direct object: %w", err)
}

package cache

import (
	"context"
	"sync"
)

type blobFillTracker struct {
	mu    sync.Mutex
	fills map[string]*blobFill
}

type blobFill struct {
	done chan struct{}
	err  error
}

func newBlobFillTracker() *blobFillTracker {
	return &blobFillTracker{fills: make(map[string]*blobFill)}
}

func (t *blobFillTracker) begin(key string) (*blobFill, bool) {
	if t == nil || key == "" {
		return nil, true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fills == nil {
		t.fills = make(map[string]*blobFill)
	}
	if fill, ok := t.fills[key]; ok {
		return fill, false
	}
	fill := &blobFill{done: make(chan struct{})}
	t.fills[key] = fill
	return fill, true
}

func (t *blobFillTracker) current(key string) (*blobFill, bool) {
	if t == nil || key == "" {
		return nil, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fill, ok := t.fills[key]
	return fill, ok
}

func (t *blobFillTracker) finish(key string, fill *blobFill, err error) {
	if t == nil || fill == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.fills != nil && t.fills[key] == fill {
		delete(t.fills, key)
	}
	fill.err = err
	close(fill.done)
}

func (f *blobFill) wait(ctx context.Context) error {
	if f == nil {
		return nil
	}
	if ctx == nil {
		return errorf("wait for blob fill context is nil")
	}
	select {
	case <-ctx.Done():
		return wrapError(ctx.Err(), "wait for blob fill")
	case <-f.done:
		return f.err
	}
}

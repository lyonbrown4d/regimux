package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

type diskCacheDirectTracker struct {
	mu            sync.Mutex
	closed        bool
	requests      map[*diskCacheDirectRequest]struct{}
	pending       sync.WaitGroup
	closeOne      sync.Once
	closeErr      error
	firstCloseErr error
}

type diskCacheDirectRequest struct {
	cancel       context.CancelFunc
	stop         func() bool
	stopOnce     sync.Once
	callbackStop bool
	callbackDone chan struct{}
	body         io.ReadCloser
	bodyDone     chan struct{}
	closingBody  bool
	closeErr     error
}

func newDiskCacheDirectTracker() *diskCacheDirectTracker {
	return &diskCacheDirectTracker{requests: make(map[*diskCacheDirectRequest]struct{})}
}

func (t *diskCacheDirectTracker) open(
	parent context.Context,
	backend Store,
	key string,
	opts GetOptions,
) (io.ReadCloser, *Info, error) {
	req, ctx, err := t.beginOpen(parent)
	if err != nil {
		return nil, nil, err
	}
	defer t.pending.Done()

	body, info, getErr := backend.Get(ctx, key, opts)
	if err := directOpenRejection(parent, ctx, getErr, body); err != nil {
		return nil, nil, t.rejectOpen(req, body, err)
	}
	if err := t.acceptOpen(parent, ctx, req, body); err != nil {
		return nil, nil, t.rejectOpen(req, body, err)
	}
	return &diskCacheDirectReader{tracker: t, request: req, body: body}, info, nil
}

func (t *diskCacheDirectTracker) beginOpen(parent context.Context) (*diskCacheDirectRequest, context.Context, error) {
	if err := parent.Err(); err != nil {
		return nil, nil, fmt.Errorf("open direct object: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	req := &diskCacheDirectRequest{
		cancel:       cancel,
		callbackDone: make(chan struct{}),
		bodyDone:     make(chan struct{}),
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		cancel()
		return nil, nil, errDiskCacheClosed
	}
	t.pending.Add(1)
	t.requests[req] = struct{}{}
	req.stop = context.AfterFunc(ctx, func() {
		defer close(req.callbackDone)
		t.closeRequestBodyAndRemember(req)
	})
	t.mu.Unlock()
	return req, ctx, nil
}

func directOpenRejection(parent, request context.Context, getErr error, body io.ReadCloser) error {
	if err := parent.Err(); err != nil {
		return fmt.Errorf("direct object caller context: %w", err)
	}
	if getErr != nil {
		return getErr
	}
	if request.Err() != nil {
		return errDiskCacheClosed
	}
	if body == nil {
		return errDiskCacheNilBody
	}
	return nil
}

func (t *diskCacheDirectTracker) acceptOpen(
	parent context.Context,
	request context.Context,
	req *diskCacheDirectRequest,
	body io.ReadCloser,
) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if parentErr := parent.Err(); parentErr != nil {
		return fmt.Errorf("direct object caller context: %w", parentErr)
	}
	if t.closed || request.Err() != nil {
		return errDiskCacheClosed
	}
	if _, active := t.requests[req]; !active {
		return errDiskCacheClosed
	}
	req.body = body
	return nil
}

func (t *diskCacheDirectTracker) rejectOpen(
	req *diskCacheDirectRequest,
	body io.ReadCloser,
	openErr error,
) error {
	if body != nil {
		if err := body.Close(); err != nil {
			closeErr := fmt.Errorf("close rejected direct object body: %w", err)
			t.rememberCloseError(req, closeErr)
			openErr = errors.Join(openErr, closeErr)
		}
	}
	t.removePendingRequest(req)
	return wrapDirectOpenError(openErr)
}

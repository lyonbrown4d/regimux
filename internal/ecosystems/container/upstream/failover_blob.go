package upstream

import (
	"context"
	"errors"
	"sync"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type attemptResult struct {
	runtime upstreamRuntime
	err     error
	attempt int
	cancel  context.CancelFunc
}

type blobFailoverRunner struct {
	client     *Client
	alias      string
	operation  string
	repository string
	digest     string
	pool       *upstreamPool
	runtimes   *collectionlist.List[upstreamRuntime]
	fn         func(context.Context, upstreamRuntime) error
	hasSuccess func() bool
	results    chan attemptResult
	req        failoverRequest
	active     map[int]context.CancelFunc

	maxAttempts int
	nextAttempt int
	inFlight    int
	mu          sync.Mutex
}

func (c *Client) doWithConcurrentFailover(
	ctx context.Context,
	req failoverRequest,
	pool *upstreamPool,
	runtimes *collectionlist.List[upstreamRuntime],
	fn func(context.Context, upstreamRuntime) error,
) (context.CancelFunc, error) {
	if pool == nil {
		return nil, c.doWithSequentialFailover(ctx, req, pool, runtimes, fn)
	}
	maxAttempts := pool.blobAttemptConcurrency()
	if maxAttempts <= 1 {
		return nil, c.doWithSequentialFailover(ctx, req, pool, runtimes, fn)
	}
	if runtimes == nil {
		return nil, c.doWithSequentialFailover(ctx, req, pool, runtimes, fn)
	}
	if maxAttempts > runtimes.Len() {
		maxAttempts = runtimes.Len()
	}

	runner := &blobFailoverRunner{
		client:      c,
		alias:       req.alias,
		operation:   req.operation,
		repository:  req.repository,
		digest:      req.digest,
		pool:        pool,
		runtimes:    runtimes,
		fn:          fn,
		hasSuccess:  req.hasConcurrentSuccess,
		results:     make(chan attemptResult, runtimes.Len()),
		req:         req,
		maxAttempts: maxAttempts,
		active:      make(map[int]context.CancelFunc, maxAttempts),
	}
	runner.startInitial(ctx)
	return runner.wait(ctx)
}

func (r *blobFailoverRunner) startInitial(ctx context.Context) {
	for range r.maxAttempts {
		if !r.startNext(ctx) {
			return
		}
	}
}

func (r *blobFailoverRunner) wait(ctx context.Context) (context.CancelFunc, error) {
	for !r.done() {
		select {
		case result := <-r.results:
			finished, winnerCancel, err := r.handleResult(ctx, result)
			if finished {
				return winnerCancel, err
			}
		case <-ctx.Done():
			r.cancelAttemptsExcept(0)
			return nil, r.doneContextError(ctx)
		}
	}
	if err := r.doneContextError(ctx); err != nil {
		return nil, err
	}

	return nil, distribution.ErrUpstream.WithDetail("all upstream blob attempts failed for " + r.alias)
}

func (r *blobFailoverRunner) startNext(ctx context.Context) bool {
	runtime, attempt, ok := r.nextRuntime()
	if !ok {
		return false
	}
	req := decorateFailoverRequest(r.req, r.pool, runtime)
	r.client.logBlobAttempt(ctx, req, runtime, attempt, r.runtimes.Len(), r.maxAttempts)
	attemptCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.active[attempt] = cancel
	r.mu.Unlock()
	go func() {
		r.results <- attemptResult{
			runtime: runtime,
			err:     runAgainstRuntime(attemptCtx, r.pool, r.operation, runtime, r.fn),
			attempt: attempt,
			cancel:  cancel,
		}
	}()
	return true
}

func (r *blobFailoverRunner) nextRuntime() (upstreamRuntime, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runtimes == nil || r.nextAttempt >= r.runtimes.Len() {
		return upstreamRuntime{}, 0, false
	}
	runtime, ok := r.runtimes.Get(r.nextAttempt)
	if !ok {
		return upstreamRuntime{}, 0, false
	}
	attempt := r.nextAttempt + 1
	r.nextAttempt++
	r.inFlight++
	return runtime, attempt, true
}

func (r *blobFailoverRunner) handleResult(ctx context.Context, result attemptResult) (bool, context.CancelFunc, error) {
	remaining, inFlightRemaining, hasNext := r.finishAttempt(result.attempt)
	req := decorateFailoverRequest(r.req, r.pool, result.runtime)
	if errors.Is(result.err, errBlobAttemptUnselected) {
		result.cancel()
		return false, nil, nil
	}
	if result.err == nil {
		r.client.recordEndpointSuccess(ctx, req, r.pool, result.runtime)
		r.client.logBlobEndpointSelected(ctx, req, result.runtime, result.attempt, r.runtimes.Len())
		r.cancelAttemptsExcept(result.attempt)
		return true, result.cancel, nil
	}
	result.cancel()
	if ctxErr := ctx.Err(); ctxErr != nil {
		r.cancelAttemptsExcept(0)
		return true, nil, wrapError(ctxErr, "upstream %s context", r.operation)
	}
	if r.hasSelectedSuccess() {
		return false, nil, nil
	}
	if !shouldFailover(req, result.err) {
		r.cancelAttemptsExcept(0)
		return true, nil, result.err
	}

	r.client.recordEndpointFailure(ctx, req, r.pool, result.runtime, result.err)
	r.client.logBlobAttemptFailure(ctx, req, result.runtime, result.err, result.attempt, r.runtimes.Len(), remaining+inFlightRemaining)
	r.client.logFailover(req, result.runtime, result.err, hasNext)
	r.client.publishFailover(ctx, req, result.runtime, result.err, hasNext)
	if hasNext {
		r.startNext(ctx)
	}
	return false, nil, nil
}

func (r *blobFailoverRunner) hasSelectedSuccess() bool {
	return r.hasSuccess != nil && r.hasSuccess()
}

func (r *blobFailoverRunner) finishAttempt(attempt int) (int, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight--
	delete(r.active, attempt)
	return r.runtimes.Len() - r.nextAttempt, r.inFlight, r.nextAttempt < r.runtimes.Len()
}

func (r *blobFailoverRunner) cancelAttemptsExcept(keep int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for attempt, cancel := range r.active {
		if attempt != keep {
			cancel()
			delete(r.active, attempt)
		}
	}
}

func (r *blobFailoverRunner) done() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextAttempt >= r.runtimes.Len() && r.inFlight == 0
}

func (r *blobFailoverRunner) doneContextError(ctx context.Context) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return wrapError(ctxErr, "upstream %s context", r.operation)
	}
	return nil
}

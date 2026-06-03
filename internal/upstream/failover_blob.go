package upstream

import (
	"context"
	"sync"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type attemptResult struct {
	runtime upstreamRuntime
	err     error
	attempt int
}

type blobFailoverRunner struct {
	client     *Client
	ctx        context.Context
	attemptCtx context.Context
	cancel     context.CancelFunc
	alias      string
	operation  string
	repository string
	digest     string
	pool       *upstreamPool
	runtimes   []upstreamRuntime
	fn         func(upstreamRuntime) error
	results    chan attemptResult

	maxAttempts int
	nextAttempt int
	inFlight    int
	mu          sync.Mutex
}

func (c *Client) doWithConcurrentFailover(
	ctx context.Context,
	req failoverRequest,
	pool *upstreamPool,
	runtimes []upstreamRuntime,
	fn func(upstreamRuntime) error,
) error {
	maxAttempts := pool.blobAttemptConcurrency()
	if maxAttempts <= 1 {
		return c.doWithSequentialFailover(ctx, req, pool, runtimes, fn)
	}
	if maxAttempts > len(runtimes) {
		maxAttempts = len(runtimes)
	}

	attemptCtx, cancel := context.WithCancel(ctx)
	runner := &blobFailoverRunner{
		client:      c,
		ctx:         ctx,
		attemptCtx:  attemptCtx,
		cancel:      cancel,
		alias:       req.alias,
		operation:   req.operation,
		repository:  req.repository,
		digest:      req.digest,
		pool:        pool,
		runtimes:    runtimes,
		fn:          fn,
		results:     make(chan attemptResult, len(runtimes)),
		maxAttempts: maxAttempts,
	}
	defer cancel()
	runner.startInitial()
	return runner.wait()
}

func (r *blobFailoverRunner) startInitial() {
	for range r.maxAttempts {
		if !r.startNext() {
			return
		}
	}
}

func (r *blobFailoverRunner) wait() error {
	for !r.done() {
		select {
		case result := <-r.results:
			finished, err := r.handleResult(result)
			if finished {
				return err
			}
		case <-r.attemptCtx.Done():
			return r.doneContextError()
		}
	}

	return distribution.ErrUpstream.WithDetail("all upstream blob attempts failed for " + r.alias)
}

func (r *blobFailoverRunner) startNext() bool {
	runtime, attempt, ok := r.nextRuntime()
	if !ok {
		return false
	}
	req := failoverRequest{alias: r.alias, operation: r.operation, repository: r.repository, digest: r.digest}
	r.client.logBlobAttempt(r.ctx, req, runtime, attempt, len(r.runtimes), r.maxAttempts)
	go func() {
		r.results <- attemptResult{
			runtime: runtime,
			err:     runAgainstRuntime(r.attemptCtx, r.pool, r.operation, runtime, r.fn),
			attempt: attempt,
		}
	}()
	return true
}

func (r *blobFailoverRunner) nextRuntime() (upstreamRuntime, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nextAttempt >= len(r.runtimes) {
		return upstreamRuntime{}, 0, false
	}
	runtime := r.runtimes[r.nextAttempt]
	attempt := r.nextAttempt + 1
	r.nextAttempt++
	r.inFlight++
	return runtime, attempt, true
}

func (r *blobFailoverRunner) handleResult(result attemptResult) (bool, error) {
	remaining, inFlightRemaining, hasNext := r.finishAttempt()
	req := failoverRequest{alias: r.alias, operation: r.operation, repository: r.repository, digest: r.digest}
	if result.err == nil {
		r.client.recordEndpointSuccess(r.ctx, req, r.pool, result.runtime)
		r.client.logBlobEndpointSelected(r.ctx, req, result.runtime, result.attempt, len(r.runtimes))
		r.cancel()
		return true, nil
	}
	if ctxErr := r.attemptCtx.Err(); ctxErr != nil {
		return true, wrapError(ctxErr, "upstream %s context", r.operation)
	}
	if !shouldFailover(req, result.err) {
		r.cancel()
		return true, result.err
	}

	r.client.recordEndpointFailure(r.ctx, req, r.pool, result.runtime, result.err)
	r.client.logBlobAttemptFailure(r.ctx, req, result.runtime, result.err, result.attempt, len(r.runtimes), remaining+inFlightRemaining)
	r.client.logFailover(req, result.runtime, result.err, hasNext)
	r.client.publishFailover(r.ctx, req, result.runtime, result.err, hasNext)
	if hasNext {
		r.startNext()
	}
	return false, nil
}

func (r *blobFailoverRunner) finishAttempt() (int, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight--
	return len(r.runtimes) - r.nextAttempt, r.inFlight, r.nextAttempt < len(r.runtimes)
}

func (r *blobFailoverRunner) done() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextAttempt >= len(r.runtimes) && r.inFlight == 0
}

func (r *blobFailoverRunner) doneContextError() error {
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return wrapError(ctxErr, "upstream %s context", r.operation)
	}
	return nil
}

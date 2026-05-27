package upstream

import (
	"context"
	"errors"
	"net/http"

	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (c *Client) doWithFailover(ctx context.Context, alias, operation string, fn func(upstreamRuntime) error) error {
	pool, err := c.upstream(alias)
	if err != nil {
		return err
	}
	runtimes := pool.runtimesForOperation(operation)
	if len(runtimes) == 0 {
		return distribution.ErrNameUnknown.WithDetail("upstream alias has no registry endpoints: " + alias)
	}
	if operation == operationBlob {
		c.logBlobEndpointPlan(ctx, alias, pool, runtimes)
		if pool.blobAttemptConcurrency() > 1 && len(runtimes) > 1 {
			return c.doWithConcurrentFailover(ctx, alias, operation, pool, runtimes, fn)
		}
	}
	return c.doWithSequentialFailover(ctx, alias, operation, pool, runtimes, fn)
}

func (c *Client) doWithSequentialFailover(ctx context.Context, alias, operation string, pool *upstreamPool, runtimes []upstreamRuntime, fn func(upstreamRuntime) error) error {
	var lastErr error
	for i := range runtimes {
		runtime := runtimes[i]
		lastErr = runAgainstRuntime(ctx, pool, operation, runtime, fn)
		if lastErr == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapError(ctxErr, "upstream %s context", operation)
		}
		if !shouldFailover(lastErr) {
			return lastErr
		}
		if operation == operationBlob {
			pool.recordProbeFailure(runtime)
		}
		c.logFailover(alias, operation, runtime, lastErr, i < len(runtimes)-1)
		c.publishFailover(ctx, alias, operation, runtime, lastErr, i < len(runtimes)-1)
	}
	return lastErr
}

func (c *Client) logBlobAttempt(ctx context.Context, alias string, runtime upstreamRuntime, attempt, total, maxAttempts int) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.DebugContext(
		ctx,
		"attempting upstream blob endpoint",
		"alias", alias,
		"registry", runtime.config.Registry,
		"attempt", attempt,
		"total_attempts", total,
		"max_attempts", maxAttempts,
	)
}

func (c *Client) logBlobAttemptFailure(ctx context.Context, alias string, runtime upstreamRuntime, err error, attempt, total, remaining int) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.WarnContext(
		ctx,
		"upstream blob endpoint failed",
		"alias", alias,
		"registry", runtime.config.Registry,
		"attempt", attempt,
		"total_attempts", total,
		"remaining_attempts", remaining,
		"error", err,
	)
}

func (c *Client) logBlobEndpointSelected(ctx context.Context, alias string, runtime upstreamRuntime, attempt, total int) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.InfoContext(
		ctx,
		"selected upstream blob endpoint",
		"alias", alias,
		"registry", runtime.config.Registry,
		"attempt", attempt,
		"total_attempts", total,
	)
}

func runAgainstRuntime(ctx context.Context, pool *upstreamPool, operation string, runtime upstreamRuntime, fn func(upstreamRuntime) error) error {
	if runtime.err != nil {
		return distribution.ErrUpstream.WithDetail(runtime.err.Error())
	}
	release, err := pool.acquireRuntime(ctx, operation, runtime)
	if err != nil {
		return err
	}
	defer release()
	return fn(runtime)
}

func (c *Client) publishFailover(ctx context.Context, alias, operation string, runtime upstreamRuntime, err error, hasNext bool) {
	if c == nil || c.events == nil {
		return
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if publishErr := events.Publish(ctx, c.events, events.UpstreamFailover{
		Alias:     alias,
		Operation: operation,
		Registry:  runtime.config.Registry,
		Error:     message,
		HasNext:   hasNext,
	}); publishErr != nil {
		return
	}
}

func (c *Client) logFailover(alias, operation string, runtime upstreamRuntime, err error, hasNext bool) {
	if !hasNext || c.logger == nil {
		return
	}
	c.logger.Warn(
		"upstream endpoint failed; trying next endpoint",
		"alias", alias,
		"operation", operation,
		"registry", runtime.config.Registry,
		"error", err,
	)
}

func (c *Client) logBlobEndpointPlan(ctx context.Context, alias string, pool *upstreamPool, runtimes []upstreamRuntime) {
	if c == nil || c.logger == nil || pool == nil {
		return
	}
	c.logger.DebugContext(ctx,
		"selected upstream endpoints for blob request",
		"alias", alias,
		"blob_mirror_policy", pool.blobPolicy,
		"blob_top_n", pool.blobTopN,
		"blob_max_concurrency_per_endpoint", pool.blobLimit,
		"endpoints", runtimeRegistries(runtimes),
	)
}

func runtimeRegistries(runtimes []upstreamRuntime) []string {
	out := make([]string, 0, len(runtimes))
	for i := range runtimes {
		out = append(out, runtimes[i].config.Registry)
	}
	return out
}

func shouldFailover(err error) bool {
	if err == nil {
		return false
	}

	if statusErr, ok := errors.AsType[*upstreamHTTPStatusError](err); ok {
		return statusErr.status == http.StatusTooManyRequests || statusErr.status >= http.StatusInternalServerError
	}

	list := distribution.FromError(err)
	if list == nil {
		return false
	}
	switch list.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false
	case http.StatusTooManyRequests:
		return true
	default:
		return list.Status >= http.StatusInternalServerError
	}
}

func (c *Client) upstream(alias string) (*upstreamPool, error) {
	if c == nil || c.upstreams == nil {
		return nil, newError("upstream registry is not configured")
	}
	pool, ok := c.upstreams.Get(alias)
	if !ok || pool == nil || len(pool.runtimes) == 0 {
		return nil, distribution.ErrNameUnknown.WithDetail("unknown upstream alias: " + alias)
	}
	return pool, nil
}

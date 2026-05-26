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
	runtimes := pool.runtimesForAttempt()
	if len(runtimes) == 0 {
		return distribution.ErrNameUnknown.WithDetail("upstream alias has no registry endpoints: " + alias)
	}

	var lastErr error
	for i := range runtimes {
		runtime := runtimes[i]
		lastErr = runAgainstRuntime(runtime, fn)
		if lastErr == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapError(ctxErr, "upstream %s context", operation)
		}
		if !shouldFailover(lastErr) {
			return lastErr
		}
		c.logFailover(alias, operation, runtime, lastErr, i < len(runtimes)-1)
		c.publishFailover(ctx, alias, operation, runtime, lastErr, i < len(runtimes)-1)
	}
	return lastErr
}

func runAgainstRuntime(runtime upstreamRuntime, fn func(upstreamRuntime) error) error {
	if runtime.err != nil {
		return distribution.ErrUpstream.WithDetail(runtime.err.Error())
	}
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

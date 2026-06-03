package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/arcgolabs/clientx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/events"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type failoverRequest struct {
	alias      string
	operation  string
	repository string
	digest     string
	sequential bool
}

func (c *Client) doWithFailover(ctx context.Context, req failoverRequest, fn func(upstreamRuntime) error) (func(), error) {
	pool, err := c.upstream(req.alias)
	if err != nil {
		return nil, err
	}
	selection := pool.selectRuntimes(req.operation, req.repository, req.digest)

	runtimes := selection.runtimes
	if runtimes == nil || runtimes.Len() == 0 {
		selection.Release()
		return nil, distribution.ErrNameUnknown.WithDetail("upstream alias has no registry endpoints: " + req.alias)
	}
	c.logUpstreamSelectionPlan(ctx, req, pool, runtimes)
	var failoverErr error
	if req.operation == operationBlob {
		if !req.sequential && pool.blobAttemptConcurrency() > 1 && runtimes.Len() > 1 {
			failoverErr = c.doWithConcurrentFailover(ctx, req, pool, runtimes.Values(), fn)
		} else {
			failoverErr = c.doWithSequentialFailover(ctx, req, pool, runtimes.Values(), fn)
		}
	} else {
		failoverErr = c.doWithSequentialFailover(ctx, req, pool, runtimes.Values(), fn)
	}
	if failoverErr != nil {
		selection.Release()
		return nil, failoverErr
	}
	return selection.Release, nil
}

func (c *Client) logUpstreamSelectionPlan(ctx context.Context, req failoverRequest, pool *upstreamPool, runtimes *collectionlist.List[upstreamRuntime]) {
	if c == nil || c.logger == nil || pool == nil || runtimes == nil || runtimes.Len() == 0 {
		return
	}

	args := c.upstreamSelectionLogArgs(req, pool, runtimes, time.Now())
	if req.operation == operationBlob {
		args = append(args,
			"blob_mirror_policy", pool.blobPolicy,
			"blob_top_n", pool.blobTopN,
			"blob_max_concurrency_per_endpoint", pool.blobLimit,
			"blob_max_concurrent_attempts", pool.blobMaxAttempts,
		)
		c.logger.DebugContext(ctx, "selected upstream endpoints for blob request", args...)
		return
	}
	c.logger.DebugContext(ctx, "selected upstream endpoints for request", args...)
}

func (c *Client) upstreamSelectionLogArgs(req failoverRequest, pool *upstreamPool, runtimes *collectionlist.List[upstreamRuntime], now time.Time) []any {
	if pool == nil {
		return []any{
			"alias", req.alias,
			"operation", req.operation,
			"repository", req.repository,
		}
	}

	entries := collectionlist.MapList(runtimes, func(_ int, runtime upstreamRuntime) string {
		snapshot := pool.health.runtimeSnapshot(runtime.config.Registry, req.repository, now)
		status := "healthy"
		if snapshot.InCooldown {
			status = "cooldown"
		}
		if snapshot.InDegraded {
			status = "degraded"
		}
		entry := fmt.Sprintf(
			"%s score=%s latency=%s inflight=%d status=%s",
			runtime.config.Registry,
			snapshot.Score,
			snapshot.LatencyEWMA,
			snapshot.Inflight,
			status,
		)
		if snapshot.HasSuccessRate {
			entry = fmt.Sprintf("%s success_rate=%.3f", entry, snapshot.SuccessRate)
		}
		return entry
	}).Values()

	args := []any{
		"alias", req.alias,
		"operation", req.operation,
		"repository", req.repository,
		"endpoints", entries,
		"selected_count", runtimes.Len(),
		"upstream_policy", pool.policy,
		"probe_enabled", pool.probeConfig.Enabled,
	}
	if req.digest != "" {
		args = append(args, "digest", req.digest)
	}
	return args
}

func (c *Client) doWithSequentialFailover(ctx context.Context, req failoverRequest, pool *upstreamPool, runtimes []upstreamRuntime, fn func(upstreamRuntime) error) error {
	var lastErr error
	for i := range runtimes {
		runtime := runtimes[i]
		lastErr = runAgainstRuntime(ctx, pool, req.operation, runtime, fn)
		if lastErr == nil {
			c.recordEndpointSuccess(ctx, req, pool, runtime)
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wrapError(ctxErr, "upstream %s context", req.operation)
		}
		if !shouldFailover(req, lastErr) {
			return lastErr
		}
		c.recordEndpointFailure(ctx, req, pool, runtime, lastErr)
		c.logFailover(req, runtime, lastErr, i < len(runtimes)-1)
		c.publishFailover(ctx, req, runtime, lastErr, i < len(runtimes)-1)
	}
	return lastErr
}

func (c *Client) logBlobAttempt(ctx context.Context, req failoverRequest, runtime upstreamRuntime, attempt, total, maxAttempts int) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.DebugContext(
		ctx,
		"attempting upstream blob endpoint",
		"alias", req.alias,
		"digest", req.digest,
		"registry", runtime.config.Registry,
		"attempt", attempt,
		"total_attempts", total,
		"max_attempts", maxAttempts,
	)
}

func (c *Client) logBlobAttemptFailure(ctx context.Context, req failoverRequest, runtime upstreamRuntime, err error, attempt, total, remaining int) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.WarnContext(
		ctx,
		"upstream blob endpoint failed",
		"alias", req.alias,
		"digest", req.digest,
		"registry", runtime.config.Registry,
		"attempt", attempt,
		"total_attempts", total,
		"remaining_attempts", remaining,
		"error", err,
	)
}

func (c *Client) logBlobEndpointSelected(ctx context.Context, req failoverRequest, runtime upstreamRuntime, attempt, total int) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.InfoContext(
		ctx,
		"selected upstream blob endpoint",
		"alias", req.alias,
		"digest", req.digest,
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

func (c *Client) publishFailover(ctx context.Context, req failoverRequest, runtime upstreamRuntime, err error, hasNext bool) {
	if c == nil || c.events == nil {
		return
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if publishErr := events.Publish(ctx, c.events, events.UpstreamFailover{
		Alias:     req.alias,
		Operation: req.operation,
		Registry:  runtime.config.Registry,
		Error:     message,
		HasNext:   hasNext,
	}); publishErr != nil {
		return
	}
}

func (c *Client) logFailover(req failoverRequest, runtime upstreamRuntime, err error, hasNext bool) {
	if !hasNext || c.logger == nil {
		return
	}
	attrs := []any{
		"alias", req.alias,
		"operation", req.operation,
		"registry", runtime.config.Registry,
		"error", err,
	}
	if req.digest != "" {
		attrs = append(attrs, "digest", req.digest)
	}
	c.logger.Warn(
		"upstream endpoint failed; trying next endpoint",
		attrs...,
	)
}

func shouldFailover(req failoverRequest, err error) bool {
	if err == nil {
		return false
	}

	if statusErr, ok := errors.AsType[*upstreamHTTPStatusError](err); ok {
		return shouldFailoverStatus(req, statusErr.status)
	}

	list := distribution.FromError(err)
	if list == nil {
		return shouldFailoverError(err)
	}
	return shouldFailoverStatus(req, list.Status)
}

func shouldFailoverError(err error) bool {
	switch clientx.KindOf(err) {
	case clientx.ErrorKindTimeout, clientx.ErrorKindTemporary, clientx.ErrorKindConnRefused, clientx.ErrorKindDNS, clientx.ErrorKindNetwork:
		return true
	case clientx.ErrorKindUnknown, clientx.ErrorKindCanceled, clientx.ErrorKindTLS, clientx.ErrorKindClosed, clientx.ErrorKindCodec:
		return false
	default:
		return false
	}
}

func shouldFailoverStatus(req failoverRequest, status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return req.operation == operationBlob && status == http.StatusNotFound
	case http.StatusTooManyRequests:
		return true
	default:
		return status >= http.StatusInternalServerError
	}
}

func (c *Client) upstream(alias string) (*upstreamPool, error) {
	if c == nil || c.upstreams == nil {
		return nil, newError("upstream registry is not configured")
	}
	pool, ok := c.upstreams.Get(alias)
	if !ok || pool == nil || pool.runtimes == nil || pool.runtimes.Len() == 0 {
		return nil, distribution.ErrNameUnknown.WithDetail("unknown upstream alias: " + alias)
	}
	return pool, nil
}

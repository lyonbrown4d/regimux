package upstream

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2"

	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func (c *Client) Probe(ctx context.Context) error {
	if c == nil || c.upstreams == nil {
		return newError("upstream registry is not configured")
	}
	var probeErr error
	c.upstreams.Range(func(alias string, _ *upstreamPool) bool {
		probeErr = errors.Join(probeErr, c.ProbeAlias(ctx, alias))
		return true
	})
	return probeErr
}

func (c *Client) ProbeAlias(ctx context.Context, alias string) error {
	pool, err := c.upstream(alias)
	if err != nil {
		return err
	}
	if !pool.probeEnabled() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var successes atomic.Int32
	var failures atomic.Int32
	tasks := make([]func(context.Context) error, 0, len(pool.runtimes))
	for _, runtime := range pool.runtimes {
		runtime := runtime
		tasks = append(tasks, func(taskCtx context.Context) error {
			if err := c.probeRuntime(taskCtx, pool, runtime); err != nil {
				failures.Add(1)
				return err
			}
			successes.Add(1)
			return nil
		})
	}
	probeErr := worker.RunAll(ctx, c.probePool(), tasks)
	successCount := int(successes.Load())
	failureCount := int(failures.Load())
	if successCount > 0 {
		c.logProbeSummary(ctx, alias, successCount, failureCount, nil)
		return nil
	}
	c.logProbeSummary(ctx, alias, successCount, failureCount, probeErr)
	return probeErr
}

func (c *Client) probePool() *ants.Pool {
	if c == nil {
		return nil
	}
	if c.workers == nil {
		return nil
	}
	return c.workers.ProbePool()
}

func (c *Client) probeRuntime(ctx context.Context, pool *upstreamPool, runtime upstreamRuntime) error {
	if runtime.err != nil {
		pool.recordProbeFailure(runtime)
		return distribution.ErrUpstream.WithDetail(runtime.err.Error())
	}

	probeCtx := ctx
	cancel := func() {}
	if timeout := runtime.config.Probe.Timeout; timeout > 0 {
		probeCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	startedAt := time.Now()
	requestURL := strings.TrimRight(runtime.config.Registry, "/") + registryAPIVersionPath
	resp, err := c.execute(probeCtx, runtime, http.MethodGet, requestURL)
	latency := time.Since(startedAt)
	if err != nil {
		pool.recordProbeFailure(runtime)
		c.logProbeResult(ctx, pool.alias, runtime, latency, err)
		return err
	}

	if probeStatusReachable(resp.StatusCode) {
		pool.recordProbeSuccess(runtime, latency)
		c.logProbeResult(ctx, pool.alias, runtime, latency, nil)
		return closeBody(resp.Body)
	}

	pool.recordProbeFailure(runtime)
	err = closeBodyWithError(resp.Body, mapStatus(resp.StatusCode, operationPing))
	c.logProbeResult(ctx, pool.alias, runtime, latency, err)
	return err
}

func probeStatusReachable(status int) bool {
	return (status >= 200 && status < 400) ||
		status == http.StatusUnauthorized ||
		status == http.StatusForbidden
}

func (c *Client) logProbeResult(ctx context.Context, alias string, runtime upstreamRuntime, latency time.Duration, err error) {
	if c == nil || c.logger == nil {
		return
	}
	args := []any{
		"alias", alias,
		"registry", runtime.config.Registry,
		"latency", latency,
	}
	if err != nil {
		c.logger.DebugContext(ctx, "upstream endpoint probe failed", append(args, "error", err)...)
		return
	}
	c.logger.DebugContext(ctx, "upstream endpoint probe completed", args...)
}

func (c *Client) logProbeSummary(ctx context.Context, alias string, successes, failures int, err error) {
	if c == nil || c.logger == nil {
		return
	}
	args := []any{
		"alias", alias,
		"successes", successes,
		"failures", failures,
	}
	if err != nil {
		c.logger.WarnContext(ctx, "upstream probe failed for all endpoints", append(args, "error", err)...)
		return
	}
	c.logger.DebugContext(ctx, "upstream probe completed", args...)
}

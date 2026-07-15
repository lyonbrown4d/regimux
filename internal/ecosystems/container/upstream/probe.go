package upstream

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/panjf2000/ants/v2"

	"github.com/lyonbrown4d/regimux/internal/ecosystem"
	"github.com/lyonbrown4d/regimux/internal/worker"
	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"go.uber.org/multierr"
)

func (c *Client) Probe(ctx context.Context) error {
	if c == nil || c.upstreams == nil {
		return newError("upstream registry is not configured")
	}
	if c.logger != nil {
		c.logger.DebugContext(ctx, "starting container upstream probe sweep", "ecosystem", ecosystem.Container)
	}
	var probeErr error
	var aliasCount int
	c.upstreams.Range(func(alias string, _ *upstreamPool) bool {
		aliasCount++
		probeErr = multierr.Append(probeErr, c.ProbeAlias(ctx, alias))
		return true
	})
	if c.logger != nil {
		c.logger.DebugContext(ctx, "completed container upstream probe sweep", "ecosystem", ecosystem.Container, "upstream_count", aliasCount)
	}
	if probeErr != nil {
		return wrapError(probeErr, "probe upstream aliases")
	}
	return nil
}

func (c *Client) ProbeAlias(ctx context.Context, alias string) error {
	pool, err := c.upstream(alias)
	if err != nil {
		return err
	}
	if c.logger != nil {
		c.logger.DebugContext(ctx, "starting container upstream probe", "ecosystem", ecosystem.Container, "alias", alias)
	}
	if !pool.probeEnabled() {
		if c.logger != nil {
			c.logger.DebugContext(ctx, "container upstream probe disabled", "ecosystem", ecosystem.Container, "alias", alias)
		}
		return nil
	}

	var successes atomic.Int32
	var failures atomic.Int32
	tasks := collectionlist.MapList(pool.runtimes, func(_ int, runtime upstreamRuntime) func(context.Context) error {
		return c.probeTask(pool, runtime, &successes, &failures)
	})
	if c.logger != nil {
		c.logger.DebugContext(ctx, "starting container upstream alias endpoint probes", "ecosystem", ecosystem.Container, "alias", alias, "endpoint_count", tasks.Len())
	}
	if tasks.IsEmpty() {
		return distribution.ErrNameUnknown.WithDetail("upstream alias has no endpoints")
	}
	probeErr := worker.RunAllSettled(ctx, c.probePool(), tasks)
	flushErr := c.FlushEndpointHealth(ctx)
	if flushErr != nil && c.logger != nil {
		c.logger.DebugContext(ctx, "upstream probe health flush incomplete", "alias", alias, "error", flushErr)
	}
	successCount := int(successes.Load())
	failureCount := int(failures.Load())
	if successCount > 0 {
		c.logProbeSummary(ctx, alias, successCount, failureCount, nil)
		return nil
	}
	probeErr = multierr.Combine(newError("probe upstream endpoints"), probeErr)
	c.logProbeSummary(ctx, alias, successCount, failureCount, probeErr)
	return wrapError(probeErr, "probe upstream alias %s", alias)
}

func (c *Client) probeTask(pool *upstreamPool, runtime upstreamRuntime, successes, failures *atomic.Int32) func(context.Context) error {
	return func(taskCtx context.Context) error {
		if err := c.probeRuntime(taskCtx, pool, runtime); err != nil {
			failures.Add(1)
			return err
		}
		successes.Add(1)
		return nil
	}
}

func (c *Client) probePool() *ants.Pool {
	if c == nil {
		return nil
	}
	if c.workers == nil {
		return nil
	}
	return c.workers.IOPool()
}

func (c *Client) probeRuntime(ctx context.Context, pool *upstreamPool, runtime upstreamRuntime) error {
	if runtime.err != nil {
		c.recordProbeFailure(ctx, pool, runtime)
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
	resp, err := c.execute(probeCtx, runtime, requestSpec{
		operation: operationPing,
		method:    http.MethodGet,
		endpoint:  requestURL,
	})
	latency := time.Since(startedAt)
	if err != nil {
		c.recordProbeFailure(ctx, pool, runtime)
		c.logProbeResult(ctx, pool.alias, runtime, latency, err)
		return err
	}

	if probeStatusReachable(resp.StatusCode) {
		c.recordProbeSuccess(ctx, pool, runtime, latency)
		c.logProbeResult(ctx, pool.alias, runtime, latency, nil)
		return closeBody(resp.Body)
	}

	c.recordProbeFailure(ctx, pool, runtime)
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
		"ecosystem", ecosystem.Container,
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
		"ecosystem", ecosystem.Container,
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

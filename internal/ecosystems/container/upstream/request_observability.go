package upstream

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/arcgolabs/clientx"
	"github.com/lyonbrown4d/regimux/internal/events"
)

func (c *Client) publishUpstreamRequest(
	ctx context.Context,
	attempt requestAttempt,
	duration time.Duration,
	err error,
) {
	if c == nil || c.events == nil {
		return
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	if publishErr := events.Publish(ctx, c.events, events.UpstreamRequest{
		Alias:     attempt.runtime.config.Alias,
		Operation: attempt.spec.operation,
		Registry:  attempt.runtime.config.Registry,
		Method:    strings.ToUpper(attempt.spec.method),
		Path:      requestPath(attempt.spec.endpoint),
		Status:    attempt.state.status,
		Attempts:  attempt.number,
		Duration:  duration,
		Size:      attempt.state.size,
		Error:     message,
	}); publishErr != nil && c.logger != nil {
		c.logger.DebugContext(ctx, "publish upstream request event failed", "error", publishErr)
	}
}

func (c *Client) logUpstreamRetry(
	ctx context.Context,
	attempt requestAttempt,
	err error,
	wait time.Duration,
) {
	if c == nil || c.logger == nil {
		return
	}
	attrs := []any{
		"alias", attempt.runtime.config.Alias,
		"operation", attempt.spec.operation,
		"method", attempt.spec.method,
		"registry", attempt.runtime.config.Registry,
		"path", requestPath(attempt.spec.endpoint),
		"attempt", attempt.number,
		"max_attempts", attempt.total,
		"wait", wait,
	}
	if attempt.state.status > 0 {
		attrs = append(attrs, "status", attempt.state.status)
	}
	if err != nil {
		attrs = append(attrs, "error", err, "error_kind", clientx.KindOf(err))
	}
	c.logger.WarnContext(ctx, "retrying upstream request", attrs...)
}

func requestPath(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Path == "" {
		return endpoint
	}
	if parsed.RawQuery == "" {
		return parsed.Path
	}
	return parsed.Path + "?" + parsed.RawQuery
}

package upstream

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/arcgolabs/clientx"
	retry "github.com/sethvargo/go-retry"
)

func maxUpstreamAttempts(cfg HTTPRetryConfig) int {
	if !cfg.Enabled {
		return 1
	}
	return max(1, cfg.MaxRetries+1)
}

func shouldRetryUpstreamStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func shouldRetryUpstreamError(err error) bool {
	switch clientx.KindOf(err) {
	case clientx.ErrorKindTimeout, clientx.ErrorKindTemporary, clientx.ErrorKindConnRefused, clientx.ErrorKindDNS, clientx.ErrorKindNetwork:
		return true
	case clientx.ErrorKindUnknown, clientx.ErrorKindCanceled, clientx.ErrorKindTLS, clientx.ErrorKindClosed, clientx.ErrorKindCodec:
		return false
	default:
		return false
	}
}

func upstreamRetryBackoff(cfg HTTPRetryConfig) retry.Backoff {
	wait := cfg.WaitMin
	if wait <= 0 {
		wait = 100 * time.Millisecond
	}
	backoff := retry.NewExponential(wait)
	if cfg.WaitMax > 0 {
		backoff = retry.WithCappedDuration(cfg.WaitMax, backoff)
	}
	return retry.WithMaxRetries(safeRetryCount(cfg.MaxRetries), backoff)
}

func safeRetryCount(maxRetries int) uint64 {
	if maxRetries <= 0 {
		return 0
	}
	retries, err := strconv.ParseUint(strconv.Itoa(maxRetries), 10, 64)
	if err != nil {
		return 0
	}
	return retries
}

func waitRetry(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return wrapError(ctx.Err(), "wait upstream retry")
	}
}

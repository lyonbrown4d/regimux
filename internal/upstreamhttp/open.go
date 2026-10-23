package upstreamhttp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// OpenStream bounds response headers separately from body progress. The opener
// must use its child context; closing the returned body cancels only that child.
func OpenStream(
	ctx context.Context,
	headerTimeout, idleTimeout time.Duration,
	open func(context.Context) (*Response, error),
) (*Response, error) {
	if headerTimeout <= 0 {
		headerTimeout = 10 * time.Second
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(headerTimeout, cancel)
	response, err := open(attemptCtx)
	if !timer.Stop() {
		err = errors.Join(err, context.DeadlineExceeded)
	}
	if contextErr := attemptCtx.Err(); contextErr != nil {
		err = errors.Join(err, contextErr)
	}
	return finishOpenStream(response, cancel, idleTimeout, err)
}

func finishOpenStream(response *Response, cancel context.CancelFunc, idleTimeout time.Duration, err error) (*Response, error) {
	if err == nil && (response == nil || response.Body == nil) {
		err = errors.New("upstream stream response has no body")
	}
	if err != nil {
		cancel()
		if response != nil && response.Body != nil {
			err = errors.Join(err, response.Body.Close())
		}
		return nil, fmt.Errorf("open upstream stream: %w", err)
	}
	response.Body = WithIdleTimeout(response.Body, cancel, idleTimeout)
	return response, nil
}

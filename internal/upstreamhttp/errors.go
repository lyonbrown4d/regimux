package upstreamhttp

import "errors"

// ErrBodyIdleTimeout identifies cancellation by the response body's idle budget.
var ErrBodyIdleTimeout = errors.New("body idle timeout")

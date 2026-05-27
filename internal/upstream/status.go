package upstream

import (
	"net/http"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type upstreamHTTPStatusError struct {
	status int
	err    error
}

func (e *upstreamHTTPStatusError) Error() string {
	if e == nil || e.err == nil {
		return "upstream returned an unsuccessful status"
	}
	return e.err.Error()
}

func (e *upstreamHTTPStatusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func mapStatus(status int, kind string) error {
	switch status {
	case http.StatusUnauthorized:
		return withUpstreamStatus(status, distribution.ErrUnauthorized)
	case http.StatusForbidden:
		return withUpstreamStatus(status, distribution.ErrDenied)
	case http.StatusNotFound:
		if kind == operationBlob {
			return withUpstreamStatus(status, distribution.ErrBlobUnknown)
		}
		return withUpstreamStatus(status, distribution.ErrManifestUnknown)
	case http.StatusTooManyRequests:
		return withUpstreamStatus(status, distribution.ErrTooManyRequests)
	default:
		if status >= 500 {
			return withUpstreamStatus(status, distribution.ErrUpstream.WithDetail(status))
		}
		return withUpstreamStatus(status, distribution.ErrUpstream.WithDetail(map[string]any{"status": status, "kind": kind}))
	}
}

func withUpstreamStatus(status int, err error) error {
	return &upstreamHTTPStatusError{status: status, err: err}
}

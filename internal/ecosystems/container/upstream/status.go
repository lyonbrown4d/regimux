package upstream

import (
	"errors"
	"net/http"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type upstreamHTTPStatusError struct {
	status int
	err    error
}

type authenticatedUpstreamStatusError struct {
	err error
}

type allowlistForbiddenError struct {
	err error
}

func (e *allowlistForbiddenError) Error() string {
	if e == nil || e.err == nil {
		return "public mirror denied the repository because it is not in the allowlist"
	}
	return e.err.Error()
}

func (e *allowlistForbiddenError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *authenticatedUpstreamStatusError) Error() string {
	if e == nil || e.err == nil {
		return "authenticated upstream returned an unsuccessful status"
	}
	return e.err.Error()
}

func (e *authenticatedUpstreamStatusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
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

// MapStatus maps an upstream HTTP status to a distribution error.
func MapStatus(status int, kind string) error {
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
			return withUpstreamStatus(status, distribution.UpstreamStatus(status, kind))
		}
		return withUpstreamStatus(status, distribution.UpstreamStatus(status, kind))
	}
}

func mapStatus(status int, kind string) error {
	return MapStatus(status, kind)
}

func withUpstreamStatus(status int, err error) error {
	return &upstreamHTTPStatusError{status: status, err: err}
}

func withAuthenticatedUpstreamStatus(status int, kind string) error {
	return &authenticatedUpstreamStatusError{
		err: withUpstreamStatus(status, MapStatus(status, kind)),
	}
}

func withAllowlistForbidden(kind string) error {
	return &allowlistForbiddenError{err: withUpstreamStatus(http.StatusForbidden, MapStatus(http.StatusForbidden, kind))}
}

func isAllowlistForbidden(err error) bool {
	allowlistErr, ok := errors.AsType[*allowlistForbiddenError](err)
	return ok && allowlistErr != nil
}

package upstream

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

type contentInconsistentError struct {
	expected string
	actual   string
	err      error
}

func (e *contentInconsistentError) Error() string {
	if e == nil || e.err == nil {
		return "upstream content digest mismatch"
	}
	return e.err.Error()
}

func (e *contentInconsistentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func validateUpstreamContentDigest(expected, actual string) error {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" || actual == "" || expected == actual {
		return nil
	}
	return &contentInconsistentError{
		expected: expected,
		actual:   actual,
		err:      distribution.DigestMismatch(expected, actual),
	}
}

func isContentInconsistent(err error) bool {
	var mismatch *contentInconsistentError
	if errors.As(err, &mismatch) {
		return true
	}
	list := distribution.FromError(err)
	if list == nil || list.Status != http.StatusBadGateway {
		return false
	}
	for _, item := range list.Errors {
		if item.Code == distribution.CodeDigestInvalid {
			return true
		}
	}
	return false
}

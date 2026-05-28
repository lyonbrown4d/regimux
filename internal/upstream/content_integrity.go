package upstream

import (
	"errors"
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
		err: distribution.ErrDigestMismatch.WithDetail(map[string]string{
			"expected": expected,
			"actual":   actual,
		}),
	}
}

func isContentInconsistent(err error) bool {
	var mismatch *contentInconsistentError
	return errors.As(err, &mismatch)
}

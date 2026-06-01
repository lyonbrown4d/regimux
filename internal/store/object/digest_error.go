package object

import "fmt"

type DigestMismatchError struct {
	Expected string
	Actual   string
}

func NewDigestMismatch(expected, actual string) *DigestMismatchError {
	return &DigestMismatchError{
		Expected: expected,
		Actual:   actual,
	}
}

func (e *DigestMismatchError) Error() string {
	if e == nil {
		return ErrDigestMismatch.Error()
	}
	return fmt.Sprintf("%s: expected %s got %s", ErrDigestMismatch, e.Expected, e.Actual)
}

func (e *DigestMismatchError) Unwrap() error {
	return ErrDigestMismatch
}

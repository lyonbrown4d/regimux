package upstream

import (
	"errors"

	"github.com/samber/oops"
)

func newError(message string) error {
	return oops.In("upstream").Wrap(errors.New(message))
}

func wrapError(err error, format string, args ...any) error {
	return oops.In("upstream").Wrapf(err, format, args...)
}

func joinError(errs ...error) error {
	return errors.Join(errs...)
}

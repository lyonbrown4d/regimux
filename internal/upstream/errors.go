package upstream

import (
	"errors"

	"github.com/samber/oops"
	"go.uber.org/multierr"
)

func newError(message string) error {
	return oops.In("upstream").Wrap(errors.New(message))
}

func wrapError(err error, format string, args ...any) error {
	return oops.In("upstream").Wrapf(err, format, args...)
}

func joinError(errs ...error) error {
	err := multierr.Combine(errs...)
	if err == nil {
		return nil
	}
	return wrapError(err, "join upstream errors")
}

package object

import (
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

func errorf(format string, args ...any) error {
	return oops.Errorf(format, args...)
}

func wrapError(err error, format string, args ...any) error {
	return oops.Wrapf(err, format, args...)
}

func joinError(message string, errs ...error) error {
	err := multierr.Combine(errs...)
	if err == nil {
		return nil
	}
	return wrapError(err, "%s", message)
}

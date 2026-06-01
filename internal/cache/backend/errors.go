package backend

import (
	"github.com/samber/oops"
	"go.uber.org/multierr"
)

func wrapError(err error, message string) error {
	return oops.Wrapf(err, "%s", message)
}

func joinError(message string, errs ...error) error {
	err := multierr.Combine(errs...)
	if err == nil {
		return nil
	}
	return wrapError(err, message)
}

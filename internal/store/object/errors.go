package object

import "github.com/samber/oops"

func errorf(format string, args ...any) error {
	return oops.Errorf(format, args...)
}

func wrapError(err error, format string, args ...any) error {
	return oops.Wrapf(err, format, args...)
}

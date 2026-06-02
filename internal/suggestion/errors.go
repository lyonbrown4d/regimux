package suggestion

import "github.com/samber/oops"

func errorf(format string, args ...any) error {
	return oops.In("suggestion").Errorf(format, args...)
}

func wrapError(err error, message string) error {
	return oops.In("suggestion").Wrapf(err, "%s", message)
}

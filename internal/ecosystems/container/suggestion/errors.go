package suggestion

import "github.com/samber/oops"

func errorf(message string) error {
	return oops.In("suggestion").Errorf("%s", message)
}

func wrapError(err error, message string) error {
	return oops.In("suggestion").Wrapf(err, "%s", message)
}

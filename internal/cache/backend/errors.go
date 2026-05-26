package backend

import "github.com/samber/oops"

func wrapError(err error, message string) error {
	return oops.Wrapf(err, "%s", message)
}

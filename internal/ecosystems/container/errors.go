package container

import "github.com/samber/oops"

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("container").Wrapf(err, "%s", message)
}

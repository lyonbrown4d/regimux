package dist

import (
	"errors"
	"net/http"

	"github.com/samber/oops"
)

var (
	errInvalidRange = errors.New("invalid dist range")
	errBlockedPath  = errors.New("dist path is not allowed")
)

func statusFromError(err error) int {
	switch {
	case errors.Is(err, errInvalidRange):
		return http.StatusRequestedRangeNotSatisfiable
	case errors.Is(err, errBlockedPath):
		return http.StatusForbidden
	default:
		return http.StatusBadGateway
	}
}

func wrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return oops.In("dist").Wrapf(err, "%s", message)
}

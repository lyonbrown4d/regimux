package pypi

import (
	"errors"
	"net/http"

	accesspolicy "github.com/lyonbrown4d/regimux/internal/policy"
)

func statusFromError(err error) int {
	if errors.Is(err, accesspolicy.ErrDependencyBlocked) {
		return http.StatusForbidden
	}
	return http.StatusBadGateway
}

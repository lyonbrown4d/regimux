package dist

import (
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

func distBodyValidator(requestRoute Route) artifactcache.BodyValidator {
	if strings.HasSuffix(strings.ToLower(requestRoute.Reference), ".zip") {
		return artifactcache.ValidateZIP
	}
	return nil
}

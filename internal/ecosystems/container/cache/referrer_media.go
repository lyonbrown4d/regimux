package cache

import (
	"encoding/json"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func validateReferrersMediaType(mediaType string) error {
	if err := distribution.ValidateReferrersMediaType(mediaType); err != nil {
		return wrapError(err, "validate referrers media type")
	}
	return nil
}

func supportedReferrersMediaType(mediaType string) bool {
	return distribution.SupportedReferrersMediaType(mediaType)
}

func validateReferrersBody(body []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return distribution.ErrUpstream.WithDetail("invalid upstream referrers index JSON: " + err.Error())
	}
	return nil
}

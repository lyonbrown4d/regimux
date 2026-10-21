package cache

import (
	"encoding/json"

	"github.com/lyonbrown4d/regimux/internal/jsonbudget"
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
	if err := jsonbudget.Check(body, jsonbudget.DefaultLimits()); err != nil {
		return distribution.ErrUpstream.WithDetail("invalid upstream referrers index JSON: " + err.Error())
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return distribution.ErrUpstream.WithDetail("invalid upstream referrers index JSON: " + err.Error())
	}
	return nil
}

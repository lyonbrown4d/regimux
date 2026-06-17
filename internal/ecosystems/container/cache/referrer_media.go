package cache

import (
	"encoding/json"
	"strconv"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func validateReferrersMediaType(mediaType string) error {
	normalized := normalizeManifestMediaType(mediaType)
	switch normalized {
	case distribution.MediaTypeOCIIndex,
		distribution.MediaTypeDockerManifestList:
		return nil
	default:
		if normalized == "" {
			normalized = "missing"
		}
		return distribution.ErrUpstream.WithDetail("unsupported upstream referrers media type: " + strconv.Quote(normalized))
	}
}

func supportedReferrersMediaType(mediaType string) bool {
	return validateReferrersMediaType(mediaType) == nil
}

func validateReferrersBody(body []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return distribution.ErrUpstream.WithDetail("invalid upstream referrers index JSON: " + err.Error())
	}
	return nil
}

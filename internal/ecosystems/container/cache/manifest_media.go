package cache

import (
	"mime"
	"strconv"
	"strings"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
)

func validateManifestMediaType(mediaType string) error {
	normalized := normalizeManifestMediaType(mediaType)
	switch normalized {
	case distribution.MediaTypeOCIIndex,
		distribution.MediaTypeOCIManifest,
		distribution.MediaTypeDockerManifestList,
		distribution.MediaTypeDockerManifest:
		return nil
	default:
		if normalized == "" {
			normalized = "missing"
		}
		return distribution.ErrUpstream.WithDetail("unsupported upstream manifest media type: " + strconv.Quote(normalized))
	}
}

func normalizeManifestMediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err == nil && mediaType != "" {
		return mediaType
	}
	return strings.ToLower(value)
}

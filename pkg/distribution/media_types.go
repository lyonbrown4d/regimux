package distribution

import (
	"mime"
	"strconv"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	MediaTypeOCIManifest        = ocispec.MediaTypeImageManifest
	MediaTypeOCIIndex           = ocispec.MediaTypeImageIndex
	MediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

func NormalizeMediaType(value string) string {
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

func ValidateManifestMediaType(mediaType string) error {
	normalized := NormalizeMediaType(mediaType)
	switch normalized {
	case MediaTypeOCIIndex,
		MediaTypeOCIManifest,
		MediaTypeDockerManifestList,
		MediaTypeDockerManifest:
		return nil
	default:
		if normalized == "" {
			normalized = "missing"
		}
		return ErrUpstream.WithDetail("unsupported upstream manifest media type: " + strconv.Quote(normalized))
	}
}

func SupportedManifestMediaType(mediaType string) bool {
	return ValidateManifestMediaType(mediaType) == nil
}

func ValidateReferrersMediaType(mediaType string) error {
	normalized := NormalizeMediaType(mediaType)
	switch normalized {
	case MediaTypeOCIIndex,
		MediaTypeDockerManifestList:
		return nil
	default:
		if normalized == "" {
			normalized = "missing"
		}
		return ErrUpstream.WithDetail("unsupported upstream referrers media type: " + strconv.Quote(normalized))
	}
}

func SupportedReferrersMediaType(mediaType string) bool {
	return ValidateReferrersMediaType(mediaType) == nil
}

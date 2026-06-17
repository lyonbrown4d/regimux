package cache

import "github.com/lyonbrown4d/regimux/pkg/distribution"

func validateManifestMediaType(mediaType string) error {
	if err := distribution.ValidateManifestMediaType(mediaType); err != nil {
		return wrapError(err, "validate manifest media type")
	}
	return nil
}

func supportedManifestMediaType(mediaType string) bool {
	return distribution.SupportedManifestMediaType(mediaType)
}

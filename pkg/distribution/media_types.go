package distribution

import ocispec "github.com/opencontainers/image-spec/specs-go/v1"

const (
	MediaTypeOCIManifest        = ocispec.MediaTypeImageManifest
	MediaTypeOCIIndex           = ocispec.MediaTypeImageIndex
	MediaTypeDockerManifest     = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerManifestList = "application/vnd.docker.distribution.manifest.list.v2+json"
)

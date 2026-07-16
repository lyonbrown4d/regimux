package maven

import (
	"io"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/artifactcache"
)

func mavenBodyValidator(requestRoute Route) artifactcache.BodyValidator {
	reference := strings.ToLower(strings.TrimSpace(requestRoute.Reference))
	switch {
	case strings.HasSuffix(reference, ".pom"):
		return validateMavenPOM
	case reference == "maven-metadata.xml":
		return validateMavenMetadata
	case strings.HasSuffix(reference, ".xml"):
		return artifactcache.ValidateXML
	default:
		return nil
	}
}

func validateMavenPOM(body io.ReaderAt, size int64) error {
	return wrapError(
		artifactcache.ValidateXMLRoot(body, size, "project"),
		"validate maven POM",
	)
}

func validateMavenMetadata(body io.ReaderAt, size int64) error {
	return wrapError(
		artifactcache.ValidateXMLRoot(body, size, "metadata"),
		"validate maven metadata",
	)
}

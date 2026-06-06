package maven

import (
	"path"
	"strings"
	"time"

	"github.com/samber/oops"
)

func ParseTail(alias, tail string) (Route, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return Route{}, oops.In("maven-proxy").Errorf("upstream alias is required")
	}
	tail = strings.Trim(strings.TrimSpace(tail), "/")
	if tail == "" {
		return Route{}, oops.In("maven-proxy").Errorf("maven path is required")
	}
	if err := validateTail(tail); err != nil {
		return Route{}, err
	}
	directory, file := path.Split(tail)
	directory = strings.Trim(directory, "/")
	if file == "" {
		return Route{}, oops.In("maven-proxy").With("path", tail).Errorf("maven path must name a file")
	}
	if directory == "" {
		directory = "_root"
	}
	kind := RouteRelease
	if strings.EqualFold(file, "maven-metadata.xml") {
		kind = RouteMetadata
	} else if isSnapshotPath(tail) {
		kind = RouteSnapshot
	}
	return Route{
		Alias:        alias,
		Kind:         kind,
		Tail:         tail,
		UpstreamTail: tail,
		Repository:   directory,
		Reference:    file,
	}, nil
}

func validateTail(tail string) error {
	for segment := range strings.SplitSeq(tail, "/") {
		switch segment {
		case "", ".", "..":
			return oops.In("maven-proxy").With("path", tail).Errorf("maven path contains an invalid segment")
		}
		if strings.Contains(segment, "\\") {
			return oops.In("maven-proxy").With("path", tail).Errorf("maven path contains an invalid segment")
		}
	}
	return nil
}

func isSnapshotPath(tail string) bool {
	for segment := range strings.SplitSeq(tail, "/") {
		if strings.Contains(strings.ToUpper(segment), "SNAPSHOT") {
			return true
		}
	}
	return false
}

func routeTTL(requestRoute Route) time.Duration {
	if requestRoute.Kind == RouteMetadata || requestRoute.Kind == RouteSnapshot {
		return defaultMetadataTTL
	}
	return 0
}

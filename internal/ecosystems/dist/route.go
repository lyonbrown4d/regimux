package dist

import (
	"path"
	"strings"

	"github.com/samber/oops"
)

func ParseTail(alias, tail string) (Route, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return Route{}, oops.In("dist").Errorf("upstream alias is required")
	}
	tail = strings.Trim(strings.TrimSpace(tail), "/")
	if err := validateTail(tail); err != nil {
		return Route{}, err
	}
	repository, reference := splitArtifactTail(tail)
	return Route{
		Alias:        alias,
		Tail:         tail,
		Repository:   repository,
		Reference:    reference,
		UpstreamTail: tail,
	}, nil
}

func validateTail(tail string) error {
	if tail == "" {
		return oops.In("dist").Errorf("dist path is required")
	}
	if strings.Contains(tail, "\\") {
		return oops.In("dist").With("path", tail).Errorf("dist path contains an invalid segment")
	}
	for segment := range strings.SplitSeq(tail, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return oops.In("dist").With("path", tail).Errorf("dist path contains an invalid segment")
		}
	}
	return nil
}

func splitArtifactTail(tail string) (string, string) {
	dir, file := path.Split(tail)
	repository := strings.Trim(dir, "/")
	if repository == "" {
		repository = "dist"
	}
	return repository, file
}

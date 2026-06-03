package pypiproxy

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/samber/oops"
)

var (
	pep503SeparatorRE = regexp.MustCompile(`[-_.]+`)
	pyPINameRE        = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

func ParseTail(alias, tail string) (Route, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return Route{}, oops.In("pypi-proxy").Errorf("upstream alias is required")
	}
	tail = strings.Trim(strings.TrimSpace(tail), "/")
	if tail == "" {
		return Route{}, oops.In("pypi-proxy").Errorf("pypi proxy path is required")
	}
	if project, ok := strings.CutPrefix(tail, "simple/"); ok {
		return parseSimpleTail(alias, tail, project)
	}
	if packageTail, ok := strings.CutPrefix(tail, "packages/"); ok {
		return parsePackageTail(alias, packageTail)
	}
	return Route{}, oops.In("pypi-proxy").With("path", tail).Errorf("pypi proxy path must start with simple/ or packages/")
}

func parseSimpleTail(alias, tail, project string) (Route, error) {
	project = strings.Trim(project, "/")
	if project == "" || strings.Contains(project, "/") {
		return Route{}, oops.In("pypi-proxy").With("path", tail).Errorf("pypi simple path must contain one project")
	}
	unescaped, err := url.PathUnescape(project)
	if err != nil {
		return Route{}, oops.In("pypi-proxy").Wrapf(err, "decode pypi project name")
	}
	normalized, err := NormalizeProjectName(unescaped)
	if err != nil {
		return Route{}, err
	}
	return Route{
		Alias:             alias,
		Kind:              RouteSimple,
		Tail:              "simple/" + normalized + "/",
		UpstreamTail:      "simple/" + normalized + "/",
		Project:           unescaped,
		NormalizedProject: normalized,
		Repository:        "pypi/simple/" + normalized,
		Reference:         "index.html",
	}, nil
}

func parsePackageTail(alias, packageTail string) (Route, error) {
	packageTail = strings.TrimLeft(packageTail, "/")
	if err := validatePackageTail(packageTail); err != nil {
		return Route{}, err
	}
	parsed := Route{
		Alias:        alias,
		Kind:         RoutePackage,
		Tail:         "packages/" + packageTail,
		UpstreamTail: packageTail,
		PackageTail:  packageTail,
		Repository:   "pypi/packages",
		Reference:    packageTail,
	}
	if absolute, ok := decodeAbsolutePackageTail(packageTail); ok {
		parsed.DirectURL = absolute.String()
	}
	return parsed, nil
}

func NormalizeProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", oops.In("pypi-proxy").Errorf("pypi project name is required")
	}
	if !pyPINameRE.MatchString(name) {
		return "", oops.In("pypi-proxy").With("project", name).Errorf("pypi project name is invalid")
	}
	return strings.ToLower(pep503SeparatorRE.ReplaceAllString(name, "-")), nil
}

func decodeAbsolutePackageTail(tail string) (*url.URL, bool) {
	scheme, rest, ok := strings.Cut(tail, "/")
	if !ok || (scheme != "http" && scheme != "https") {
		return nil, false
	}
	host, path, ok := strings.Cut(rest, "/")
	if !ok || host == "" || path == "" {
		return nil, false
	}
	return &url.URL{Scheme: scheme, Host: host, Path: "/" + path}, true
}

func validatePackageTail(tail string) error {
	if tail == "" {
		return oops.In("pypi-proxy").Errorf("pypi package path is required")
	}
	for segment := range strings.SplitSeq(tail, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return oops.In("pypi-proxy").With("path", tail).Errorf("pypi package path contains an invalid segment")
		}
	}
	return nil
}

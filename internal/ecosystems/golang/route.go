package golang

import (
	"strings"
	"time"

	"github.com/samber/oops"
)

const (
	routeVersionMarker = "/@v/"
	routeLatestSuffix  = "/@latest"
)

type route struct {
	Alias     string
	Tail      string
	Module    string
	Reference string
}

func parseRoute(alias, tail string) (route, error) {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return route{}, oops.In("go-proxy").Errorf("upstream alias is required")
	}
	parsed, err := parseRootRoute(tail)
	if err != nil {
		return parsed, err
	}
	parsed.Alias = alias
	return parsed, nil
}

func parseRootRoute(tail string) (route, error) {
	tail = strings.Trim(strings.TrimSpace(tail), "/")
	if tail == "" {
		return route{}, oops.In("go-proxy").Errorf("go proxy path is required")
	}
	if err := validateTail(tail); err != nil {
		return route{}, err
	}
	if module, ok := strings.CutSuffix(tail, routeLatestSuffix); ok {
		if module == "" {
			return route{}, oops.In("go-proxy").Errorf("module path is required")
		}
		return route{Tail: tail, Module: module, Reference: "@latest"}, nil
	}
	module, file, ok := strings.Cut(tail, routeVersionMarker)
	if !ok || module == "" || file == "" {
		return route{}, oops.In("go-proxy").Errorf("go proxy path must contain /@v/ or end with /@latest")
	}
	return route{
		Tail:      tail,
		Module:    module,
		Reference: "@v/" + file,
	}, nil
}

func validateTail(tail string) error {
	for segment := range strings.SplitSeq(tail, "/") {
		switch segment {
		case "", ".", "..":
			return oops.In("go-proxy").With("path", tail).Errorf("go proxy path contains an invalid segment")
		}
	}
	return nil
}

func isGoProxyTail(tail string) bool {
	tail = strings.Trim(strings.TrimSpace(tail), "/")
	return strings.Contains(tail, routeVersionMarker) || strings.HasSuffix(tail, routeLatestSuffix)
}

func routeCacheable(r route) bool {
	if r.Reference == "@latest" || r.Reference == "@v/list" {
		return true
	}
	return strings.HasSuffix(r.Reference, ".info") ||
		strings.HasSuffix(r.Reference, ".mod") ||
		strings.HasSuffix(r.Reference, ".zip")
}

func routeMetadataTTL(r route, ttl time.Duration) time.Duration {
	if r.Reference == "@latest" || r.Reference == "@v/list" {
		return ttl
	}
	return 0
}

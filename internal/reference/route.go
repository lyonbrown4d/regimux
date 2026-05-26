package reference

import (
	"errors"
	"regexp"
	"strings"

	"github.com/samber/oops"
)

var (
	errPathInvalid   = errors.New("invalid registry path")
	aliasRegexp      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	repoSegmentRegex = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
	tagRegexp        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// UpstreamRepo returns the repository name used against the upstream registry.
func (r Route) UpstreamRepo() string {
	return r.Repo
}

// ParsePath parses the supported read-only Registry V2 paths.
func ParsePath(path string) (*Route, error) {
	path = strings.TrimSpace(path)
	if isPingPath(path) {
		return &Route{Kind: RoutePing}, nil
	}
	if err := validateRegistryPath(path); err != nil {
		return nil, err
	}
	return parseOperationPath(path)
}

func isPingPath(path string) bool {
	return path == "/v2" || path == "/v2/"
}

func validateRegistryPath(path string) error {
	if !strings.HasPrefix(path, "/v2/") {
		return oops.Wrapf(errPathInvalid, "path must start with /v2/")
	}
	if strings.Contains(path, "?") || strings.Contains(path, "#") {
		return oops.Wrapf(errPathInvalid, "path must not include query or fragment")
	}
	if hasEmptyDotOrDotDotSegment(path) {
		return oops.Wrapf(errPathInvalid, "empty, dot, and dot-dot path segments are not allowed")
	}
	return nil
}

func parseOperationPath(path string) (*Route, error) {
	switch detectOperation(path) {
	case RoutePing:
		return nil, oops.Wrapf(errPathInvalid, "unsupported registry operation")
	case RouteReferrers:
		return parseReferrersPath(path)
	case RouteTags:
		return parseTagsPath(path)
	case RouteManifest:
		return parseManifestPath(path)
	case RouteBlob:
		return parseBlobPath(path)
	}
	return nil, oops.Wrapf(errPathInvalid, "unsupported registry operation")
}

func ParseManifestPath(path string) (*Route, error) {
	route, err := parseManifestPath(path)
	if err != nil {
		return nil, err
	}
	return route, nil
}

func ParseBlobPath(path string) (*Route, error) {
	route, err := parseBlobPath(path)
	if err != nil {
		return nil, err
	}
	return route, nil
}

func ParseTagsPath(path string) (*Route, error) {
	route, err := parseTagsPath(path)
	if err != nil {
		return nil, err
	}
	return route, nil
}

func ParseReferrersPath(path string) (*Route, error) {
	route, err := parseReferrersPath(path)
	if err != nil {
		return nil, err
	}
	return route, nil
}

func ParsePingPath(path string) (*Route, error) {
	path = strings.TrimSpace(path)
	if path != "/v2" && path != "/v2/" {
		return nil, oops.Wrapf(errPathInvalid, "not a ping path")
	}
	return &Route{Kind: RoutePing}, nil
}

func parseManifestPath(path string) (*Route, error) {
	name, reference, ok := splitOperationPath(path, "/manifests/")
	if !ok {
		return nil, oops.Wrapf(errPathInvalid, "not a manifest path")
	}
	normalized, err := normalizeReference(reference)
	if err != nil {
		return nil, err
	}
	route, err := routeFromName(RouteManifest, name)
	if err != nil {
		return nil, err
	}
	route.Reference = normalized
	return route, nil
}

func parseBlobPath(path string) (*Route, error) {
	name, digest, ok := splitOperationPath(path, "/blobs/")
	if !ok {
		return nil, oops.Wrapf(errPathInvalid, "not a blob path")
	}
	digest, err := NormalizeDigest(digest)
	if err != nil {
		return nil, err
	}
	route, err := routeFromName(RouteBlob, name)
	if err != nil {
		return nil, err
	}
	route.Digest = digest
	return route, nil
}

func parseTagsPath(path string) (*Route, error) {
	const marker = "/tags/list"
	if !strings.HasSuffix(path, marker) {
		return nil, oops.Wrapf(errPathInvalid, "not a tags path")
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, "/v2/"), marker)
	route, err := routeFromName(RouteTags, name)
	if err != nil {
		return nil, err
	}
	return route, nil
}

func parseReferrersPath(path string) (*Route, error) {
	name, digest, ok := splitOperationPath(path, "/referrers/")
	if !ok {
		return nil, oops.Wrapf(errPathInvalid, "not a referrers path")
	}
	digest, err := NormalizeDigest(digest)
	if err != nil {
		return nil, err
	}
	route, err := routeFromName(RouteReferrers, name)
	if err != nil {
		return nil, err
	}
	route.Digest = digest
	return route, nil
}

func splitOperationPath(path, marker string) (name, tail string, ok bool) {
	if !strings.HasPrefix(path, "/v2/") {
		return "", "", false
	}
	idx := strings.LastIndex(path, marker)
	if idx < 0 {
		return "", "", false
	}
	name = strings.TrimPrefix(path[:idx], "/v2/")
	tail = path[idx+len(marker):]
	if name == "" || tail == "" || strings.Contains(tail, "/") {
		return "", "", false
	}
	return name, tail, true
}

func detectOperation(path string) RouteKind {
	detected, maxIndex := detectMarkedOperation(path)
	return detectTagsOperation(path, detected, maxIndex)
}

func detectMarkedOperation(path string) (RouteKind, int) {
	var detected RouteKind
	maxIndex := -1
	for _, candidate := range operationMarkers() {
		idx := strings.LastIndex(path, candidate.marker)
		if idx < 0 || !hasValidOperationTail(path, idx, candidate.marker) {
			continue
		}
		if idx > maxIndex {
			maxIndex = idx
			detected = candidate.kind
		}
	}
	return detected, maxIndex
}

func detectTagsOperation(path string, detected RouteKind, maxIndex int) RouteKind {
	const tagsMarker = "/tags/list"
	if strings.HasSuffix(path, tagsMarker) {
		idx := strings.LastIndex(path, tagsMarker)
		if idx > maxIndex {
			detected = RouteTags
		}
	}
	return detected
}

func operationMarkers() []struct {
	kind   RouteKind
	marker string
} {
	return []struct {
		kind   RouteKind
		marker string
	}{
		{kind: RouteReferrers, marker: "/referrers/"},
		{kind: RouteManifest, marker: "/manifests/"},
		{kind: RouteBlob, marker: "/blobs/"},
	}
}

func hasValidOperationTail(path string, idx int, marker string) bool {
	tail := path[idx+len(marker):]
	return tail != "" && !strings.Contains(tail, "/")
}

func routeFromName(kind RouteKind, name string) (*Route, error) {
	alias, repo, ok := strings.Cut(name, "/")
	if !ok || alias == "" || repo == "" {
		return nil, oops.Wrapf(errPathInvalid, "path must include alias and repository")
	}
	if !aliasRegexp.MatchString(alias) {
		return nil, oops.Wrapf(errPathInvalid, "invalid upstream alias %q", alias)
	}
	if err := validateRepo(repo); err != nil {
		return nil, err
	}
	return &Route{Kind: kind, Alias: alias, Repo: repo}, nil
}

func validateRepo(repo string) error {
	if repo == "" {
		return oops.Wrapf(errPathInvalid, "empty repository")
	}
	for segment := range strings.SplitSeq(repo, "/") {
		if !repoSegmentRegex.MatchString(segment) {
			return oops.Wrapf(errPathInvalid, "invalid repository segment %q", segment)
		}
	}
	return nil
}

func normalizeReference(reference string) (string, error) {
	if reference == "" || strings.Contains(reference, "/") {
		return "", oops.Wrapf(errPathInvalid, "invalid manifest reference")
	}
	if digest, err := NormalizeDigest(reference); err == nil {
		return digest, nil
	}
	if tagRegexp.MatchString(reference) {
		return reference, nil
	}
	return "", oops.Wrapf(errPathInvalid, "invalid manifest reference %q", reference)
}

func hasEmptyDotOrDotDotSegment(path string) bool {
	segments := strings.SplitSeq(path, "/")
	for segment := range segments {
		if segment == "" {
			continue
		}
		if segment == "." || segment == ".." {
			return true
		}
	}
	return strings.Contains(path, "//")
}

package npmproxy

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/pkg/distribution"
	"github.com/samber/oops"
)

func parseRoute(req Request) (route, error) {
	alias := strings.TrimSpace(req.Alias)
	if alias == "" {
		return route{}, oops.In("npm-proxy").Errorf("upstream alias is required")
	}
	tail := strings.Trim(strings.TrimSpace(req.Tail), "/")
	if tail == "" {
		return route{}, oops.In("npm-proxy").Errorf("npm proxy path is required")
	}
	if err := validateTail(tail); err != nil {
		return route{}, err
	}
	decodedTail, err := url.PathUnescape(tail)
	if err != nil {
		return route{}, oops.In("npm-proxy").With("path", tail).Wrapf(err, "decode npm proxy path")
	}
	requestRoute := route{
		Alias: alias,
		Tail:  tail,
		Query: req.Query,
	}
	if pkg, tarball, ok := parseTarballPath(decodedTail); ok {
		requestRoute.Kind = routeTarball
		requestRoute.Package = pkg
		requestRoute.Reference = "tarball:" + tarball
		requestRoute.UpstreamTail = encodeTarballTail(decodedTail)
		return requestRoute, nil
	}
	if pkg, ok := parseMetadataPath(decodedTail); ok {
		requestRoute.Kind = routeMetadata
		requestRoute.Package = pkg
		requestRoute.Reference = metadataRef
		requestRoute.UpstreamTail = encodeMetadataTail(pkg)
		return requestRoute, nil
	}
	requestRoute.Kind = routeOther
	requestRoute.Package = packageFromTail(decodedTail)
	requestRoute.Reference = "path:" + decodedTail
	requestRoute.UpstreamTail = encodePath(decodedTail)
	return requestRoute, nil
}

func cacheable(r route) bool {
	return r.Kind == routeMetadata || r.Kind == routeTarball
}

func routeTTL(r route, metadataTTL time.Duration) time.Duration {
	if r.Kind != routeMetadata {
		return 0
	}
	if metadataTTL > 0 {
		return metadataTTL
	}
	return defaultMetadataTTL
}

func contentType(r route, headers http.Header) string {
	if value := headers.Get(distribution.HeaderContentType); value != "" {
		return value
	}
	if r.Kind == routeMetadata {
		return distribution.MediaTypeJSON
	}
	return tarballMedia
}

func validateTail(tail string) error {
	for segment := range strings.SplitSeq(tail, "/") {
		switch segment {
		case "", ".", "..":
			return oops.In("npm-proxy").With("path", tail).Errorf("npm proxy path contains an invalid segment")
		}
	}
	return nil
}

func parseMetadataPath(decodedTail string) (string, bool) {
	if decodedTail == "" || strings.Contains(decodedTail, "/-/") || strings.HasSuffix(decodedTail, ".tgz") {
		return "", false
	}
	if strings.HasPrefix(decodedTail, "@") {
		parts := strings.Split(decodedTail, "/")
		if len(parts) == 2 && parts[0] != "@" && parts[1] != "" {
			return decodedTail, true
		}
		return "", false
	}
	if !strings.Contains(decodedTail, "/") {
		return decodedTail, true
	}
	return "", false
}

func parseTarballPath(decodedTail string) (string, string, bool) {
	pkgPart, filePart, ok := strings.Cut(decodedTail, "/-/")
	if !ok || pkgPart == "" || filePart == "" || !strings.HasSuffix(filePart, ".tgz") {
		return "", "", false
	}
	if strings.HasPrefix(pkgPart, "@") {
		parts := strings.Split(pkgPart, "/")
		if len(parts) != 2 || parts[1] == "" {
			return "", "", false
		}
	}
	if strings.Contains(pkgPart, "/") && !strings.HasPrefix(pkgPart, "@") {
		return "", "", false
	}
	return pkgPart, filePart, true
}

func packageFromTail(decodedTail string) string {
	if strings.HasPrefix(decodedTail, "@") {
		parts := strings.Split(decodedTail, "/")
		if len(parts) >= 2 {
			return parts[0] + "/" + parts[1]
		}
	}
	if first, _, ok := strings.Cut(decodedTail, "/"); ok {
		return first
	}
	return decodedTail
}

func encodeMetadataTail(pkg string) string {
	if strings.HasPrefix(pkg, "@") {
		scope, name, ok := strings.Cut(pkg, "/")
		if ok {
			return scope + "%2f" + url.PathEscape(name)
		}
	}
	return encodePath(pkg)
}

func encodeTarballTail(decodedTail string) string {
	pkg, file, ok := strings.Cut(decodedTail, "/-/")
	if !ok {
		return encodePath(decodedTail)
	}
	return encodePath(pkg) + "/-/" + url.PathEscape(file)
}

func encodePath(value string) string {
	parts := strings.Split(value, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

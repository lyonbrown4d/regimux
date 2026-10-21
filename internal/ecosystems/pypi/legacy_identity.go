package pypi

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
)

const legacyIdentityOverallTimeout = 10 * time.Second

var legacySDistVersionRE = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*(?:[-_.]?(?:a|b|rc|alpha|beta|pre|preview|post|dev)[0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z.-]+)?$`)

// legacyPackageIdentity recovers a trusted artifact identity for package URLs
// emitted by older simple-index rewrites that did not carry proxy query metadata.
func (s *Service) legacyPackageIdentity(ctx context.Context, cfg config.UpstreamConfig, route Route) (string, string) {
	overallCtx, cancelOverall := context.WithTimeout(ctx, legacyIdentityOverallTimeout)
	defer cancelOverall()
	if overallCtx.Err() != nil {
		return "", ""
	}
	requestedURL, ok := legacyRequestedURL(route)
	if !ok {
		return "", ""
	}
	filename := path.Base(requestedURL.Path)
	project := legacyPackageProject(filename)
	if project == "" {
		return "", ""
	}
	if digest := s.cachedLegacyPackageDigest(overallCtx, route.Alias, project, requestedURL, filename); digest != "" {
		return project, digest
	}
	if overallCtx.Err() != nil {
		return "", ""
	}
	if digest := s.fetchLegacyPackageDigest(overallCtx, cfg, project, requestedURL, filename); digest != "" {
		return project, digest
	}
	return "", ""
}

func (s *Service) cachedLegacyPackageDigest(ctx context.Context, alias, project string, requestedURL *url.URL, filename string) string {
	simpleRoute, err := ParseTail(alias, "simple/"+url.PathEscape(project)+"/")
	if err != nil {
		return ""
	}
	cached, cachedOK, err := s.cached(ctx, simpleRoute)
	if err != nil || !cachedOK || cached.body == nil {
		return ""
	}
	defer closeReadCloser(cached.body, s.logger, "close cached pypi index during legacy identity lookup")
	var digest string
	s.withSimpleIndexAdmission(ctx, cached.body, func(body []byte) {
		digest = cachedLegacyIdentity(body, alias, requestedURL, filename)
	})
	return digest
}

func (s *Service) fetchLegacyPackageDigest(ctx context.Context, cfg config.UpstreamConfig, project string, requestedURL *url.URL, filename string) string {
	attemptTimeout := cfg.HTTP.AttemptTimeout
	if attemptTimeout <= 0 || attemptTimeout > legacyIdentityOverallTimeout {
		attemptTimeout = legacyIdentityOverallTimeout
	}
	for _, indexURL := range configuredPyPIMirrorIndexes(cfg, project) {
		if ctx.Err() != nil {
			return ""
		}
		if digest := s.fetchLegacyIndexDigest(ctx, cfg, indexURL, attemptTimeout, requestedURL, filename); digest != "" {
			return digest
		}
	}
	return ""
}

func (s *Service) fetchLegacyIndexDigest(ctx context.Context, cfg config.UpstreamConfig, indexURL string, timeout time.Duration, requestedURL *url.URL, filename string) string {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	headers := make(http.Header)
	headers.Set("User-Agent", defaultUserAgent)
	resp, err := s.doFetch(attemptCtx, cfg, indexURL, upstreamhttp.Request{Method: http.MethodGet, URL: indexURL, Headers: headers, Auth: cfg.Auth})
	if resp == nil || resp.Body == nil {
		return ""
	}
	defer closeReadCloser(resp.Body, s.logger, "close fetched pypi index during legacy identity lookup")
	if err != nil || resp.Status != http.StatusOK {
		return ""
	}
	var digest string
	s.withSimpleIndexAdmission(attemptCtx, resp.Body, func(body []byte) {
		digest = rawIndexLegacyIdentity(body, indexURL, requestedURL, filename)
	})
	return digest
}

func legacyRequestedURL(route Route) (*url.URL, bool) {
	if route.DirectURL == "" {
		return nil, false
	}
	parsed, err := url.Parse(route.DirectURL)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return nil, false
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.RawQuery = stripPyPIProxyQuery(route.Query)
	return parsed, true
}

func legacyPackageProject(filename string) string {
	lowerFilename := strings.ToLower(filename)
	if strings.HasSuffix(lowerFilename, ".whl") {
		return normalizeLegacyProject(strings.SplitN(filename, "-", 2)[0])
	}
	sourceName, ok := legacySourceName(filename, lowerFilename)
	if !ok {
		return ""
	}
	return normalizeLegacyProject(legacySDistProjectPrefix(sourceName))
}

func legacySourceName(filename, lowerFilename string) (string, bool) {
	for _, suffix := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".zip"} {
		if strings.HasSuffix(lowerFilename, suffix) {
			return filename[:len(filename)-len(suffix)], true
		}
	}
	return "", false
}

func legacySDistProjectPrefix(sourceName string) string {
	for separator := strings.LastIndex(sourceName, "-"); separator > 0; separator = strings.LastIndex(sourceName[:separator], "-") {
		if legacySDistVersionRE.MatchString(sourceName[separator+1:]) {
			return sourceName[:separator]
		}
	}
	return ""
}

func normalizeLegacyProject(projectName string) string {
	project, err := NormalizeProjectName(projectName)
	if err != nil {
		return ""
	}
	return project
}

func cachedLegacyIdentity(body []byte, alias string, requestedURL *url.URL, filename string) string {
	return firstLegacyAnchorDigest(body, func(href string) string {
		return cachedLegacyLinkDigest(alias, href, requestedURL, filename)
	})
}

func cachedLegacyLinkDigest(alias, href string, requestedURL *url.URL, filename string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	marker := "/pypi/" + url.PathEscape(alias) + "/"
	index := strings.LastIndex(parsed.Path, marker)
	if index < 0 {
		return ""
	}
	tail := parsed.Path[index+len(marker):]
	route, err := ParseTail(alias, tail)
	if err != nil || route.DirectURL == "" {
		return ""
	}
	artifactURL, err := url.Parse(route.DirectURL)
	if err != nil || artifactURL == nil || path.Base(artifactURL.Path) != filename {
		return ""
	}
	artifactURL.RawQuery = stripPyPIProxyQuery(parsed.RawQuery)
	if !sameLegacyArtifactURL(artifactURL, requestedURL) {
		return ""
	}
	return legacySHA256(parsed.Fragment)
}

func rawIndexLegacyIdentity(body []byte, indexURL string, requestedURL *url.URL, filename string) string {
	base, err := url.Parse(indexURL)
	if err != nil {
		return ""
	}
	return firstLegacyAnchorDigest(body, func(href string) string {
		return rawLegacyLinkDigest(base, href, requestedURL, filename)
	})
}

func firstLegacyAnchorDigest(body []byte, digestForHref func(string) string) string {
	var digest string
	forEachPyPIAnchor(body, func(href string) bool {
		digest = digestForHref(href)
		return digest != ""
	})
	return digest
}

func rawLegacyLinkDigest(base *url.URL, href string, requestedURL *url.URL, filename string) string {
	link, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(link)
	if (!strings.EqualFold(resolved.Scheme, "http") && !strings.EqualFold(resolved.Scheme, "https")) ||
		resolved.Host == "" || resolved.Hostname() == "" || resolved.User != nil || path.Base(resolved.Path) != filename {
		return ""
	}
	resolved.RawQuery = stripPyPIProxyQuery(resolved.RawQuery)
	if !sameLegacyArtifactURL(resolved, requestedURL) {
		return ""
	}
	return legacySHA256(link.Fragment)
}

func sameLegacyArtifactURL(left, right *url.URL) bool {
	return left != nil && right != nil &&
		strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host) &&
		left.EscapedPath() == right.EscapedPath() &&
		left.RawQuery == right.RawQuery
}

func stripPyPIProxyQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parts := strings.Split(rawQuery, "&")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		key := part
		if before, _, ok := strings.Cut(part, "="); ok {
			key = before
		}
		decodedKey, err := url.QueryUnescape(key)
		if err == nil && strings.HasPrefix(decodedKey, "__regimux_") {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, "&")
}

func legacySHA256(fragment string) string {
	value, ok := strings.CutPrefix(fragment, "sha256=")
	if !ok || !validSHA256(value) {
		return ""
	}
	return strings.ToLower(value)
}

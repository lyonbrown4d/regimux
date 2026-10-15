package pypi

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/lyonbrown4d/regimux/internal/upstreamhttp"
	"golang.org/x/net/html"
)

const (
	pypiMirrorIndexLimit     = 8
	pypiMirrorCandidateLimit = 8
	pypiMirrorDefaultTimeout = 10 * time.Second
)

// resolvePackageMirrors uses configured PyPI simple indexes to discover alternate
// locations for an artifact only when the original index supplied its identity.
func (s *Service) resolvePackageMirrors(ctx context.Context, cfg config.UpstreamConfig, alias, project, filename, sha256 string) []string {
	_ = alias // The configuration is already scoped to this upstream alias.
	project, err := NormalizeProjectName(project)
	if err != nil || strings.TrimSpace(filename) == "" || !validSHA256(sha256) {
		return nil
	}

	candidates := make([]string, 0, pypiMirrorCandidateLimit)
	seen := make(map[string]struct{}, pypiMirrorCandidateLimit)
	for _, indexURL := range configuredPyPIMirrorIndexes(cfg, project) {
		if ctx.Err() != nil {
			break
		}
		candidates = appendUniqueMirrorCandidates(candidates, seen, s.fetchPackageMirrorLinks(ctx, cfg, indexURL, filename, sha256))
		if len(candidates) >= pypiMirrorCandidateLimit {
			break
		}
	}
	return candidates
}

func configuredPyPIMirrorIndexes(cfg config.UpstreamConfig, project string) []string {
	endpoints := make([]string, 0, 1+len(cfg.Mirrors))
	if strings.TrimSpace(cfg.Registry) != "" {
		endpoints = append(endpoints, cfg.Registry)
	}
	endpoints = append(endpoints, cfg.Mirrors...)
	if len(endpoints) > pypiMirrorIndexLimit {
		endpoints = endpoints[:pypiMirrorIndexLimit]
	}
	indexes := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if indexURL, ok := pypiMirrorIndexURL(endpoint, project); ok {
			indexes = append(indexes, indexURL)
		}
	}
	return indexes
}

func appendUniqueMirrorCandidates(candidates []string, seen map[string]struct{}, additions []string) []string {
	for _, candidate := range additions {
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
		if len(candidates) >= pypiMirrorCandidateLimit {
			break
		}
	}
	return candidates
}

func (s *Service) fetchPackageMirrorLinks(ctx context.Context, cfg config.UpstreamConfig, indexURL, filename, sha256 string) []string {
	timeout := cfg.HTTP.AttemptTimeout
	if timeout <= 0 {
		timeout = pypiMirrorDefaultTimeout
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	headers := make(http.Header)
	headers.Set("User-Agent", defaultUserAgent)
	resp, err := s.doFetch(attemptCtx, cfg, indexURL, upstreamhttp.Request{
		Method:  http.MethodGet,
		URL:     indexURL,
		Headers: headers,
		Auth:    cfg.Auth,
	})
	if resp == nil || resp.Body == nil {
		return nil
	}
	defer closeReadCloser(resp.Body, s.logger, "close pypi mirror index response body")
	if err != nil || resp.Status != http.StatusOK {
		return nil
	}
	body, err := upstreamhttp.ReadAllLimited(resp.Body, pypiSimpleIndexMaxBytes)
	if err != nil {
		return nil
	}
	return pypiMirrorLinks(body, indexURL, filename, sha256)
}

func pypiMirrorIndexURL(endpoint, project string) (string, bool) {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Opaque != "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil {
		return "", false
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", false
	}

	endpointPath := parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
	tail := upstreamTail(endpointPath, Route{
		Kind:         RouteSimple,
		UpstreamTail: "simple/" + url.PathEscape(project) + "/",
	})
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(tail, "/")
	parsed.RawPath = ""
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String(), true
}

func pypiMirrorLinks(body []byte, indexURL, filename, sha256 string) []string {
	base, err := url.Parse(indexURL)
	if err != nil {
		return nil
	}

	links := make([]string, 0, pypiMirrorCandidateLimit)
	seen := make(map[string]struct{}, pypiMirrorCandidateLimit)
	forEachPyPIAnchor(body, func(href string) bool {
		candidate, ok := pypiMirrorLink(base, href, filename, sha256)
		if !ok {
			return false
		}
		if _, exists := seen[candidate]; exists {
			return false
		}
		seen[candidate] = struct{}{}
		links = append(links, candidate)
		return len(links) >= pypiMirrorCandidateLimit
	})
	return links
}

func forEachPyPIAnchor(body []byte, visit func(string) bool) {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			return
		}
		if tokenType != html.StartTagToken && tokenType != html.SelfClosingTagToken {
			continue
		}
		if visitPyPIAnchor(tokenizer.Token(), visit) {
			return
		}
	}
}

func visitPyPIAnchor(token html.Token, visit func(string) bool) bool {
	if !strings.EqualFold(token.Data, "a") {
		return false
	}
	for _, attr := range token.Attr {
		if strings.EqualFold(attr.Key, "href") && visit(attr.Val) {
			return true
		}
	}
	return false
}

func pypiMirrorLink(base *url.URL, href, filename, sha256 string) (string, bool) {
	href = strings.TrimSpace(href)
	parsed, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(parsed)
	if !validPyPIMirrorArtifactURL(resolved, filename) {
		return "", false
	}
	if !strings.HasPrefix(parsed.Fragment, "sha256=") || !strings.EqualFold(strings.TrimPrefix(parsed.Fragment, "sha256="), sha256) {
		return "", false
	}
	resolved.Fragment = ""
	resolved.RawFragment = ""
	return resolved.String(), true
}

func validPyPIMirrorArtifactURL(resolved *url.URL, filename string) bool {
	return (strings.EqualFold(resolved.Scheme, "http") || strings.EqualFold(resolved.Scheme, "https")) &&
		resolved.Host != "" && resolved.Hostname() != "" && resolved.User == nil && path.Base(resolved.Path) == filename
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

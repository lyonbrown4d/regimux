package pypiproxy

import (
	"net/url"
	"regexp"
	"strings"
)

var hrefRE = regexp.MustCompile(`(?i)(href\s*=\s*)(["'])([^"']+)(["'])`)

func RewriteSimpleIndexLinks(body []byte, alias, upstreamSimpleURL string) []byte {
	return rewriteSimpleIndexLinks(body, alias, upstreamSimpleURL, "")
}

func rewriteSimpleIndexLinks(body []byte, alias, upstreamSimpleURL, publicURL string) []byte {
	if len(body) == 0 {
		return body
	}
	upstreamURL, err := url.Parse(upstreamSimpleURL)
	if err != nil {
		return body
	}
	rewritten := hrefRE.ReplaceAllFunc(body, func(match []byte) []byte {
		parts := hrefRE.FindSubmatch(match)
		if len(parts) != 5 {
			return match
		}
		href := string(parts[3])
		local := localPackageHref(alias, upstreamURL, href)
		if local == "" {
			return match
		}
		if publicURL != "" {
			local = strings.TrimRight(publicURL, "/") + local
		}
		out := make([]byte, 0, len(parts[1])+len(parts[2])*2+len(local))
		out = append(out, parts[1]...)
		out = append(out, parts[2]...)
		out = append(out, local...)
		out = append(out, parts[4]...)
		return out
	})
	return rewritten
}

func localPackageHref(alias string, upstreamSimpleURL *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/pypi/") {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := upstreamSimpleURL.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	path := strings.TrimLeft(resolved.EscapedPath(), "/")
	if path == "" {
		return ""
	}
	local := "/pypi/" + url.PathEscape(alias) + "/packages/" + resolved.Scheme + "/" + resolved.Host + "/" + path
	if resolved.RawQuery != "" {
		local += "?" + resolved.RawQuery
	}
	if parsed.Fragment != "" {
		local += "#" + parsed.Fragment
	}
	return local
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

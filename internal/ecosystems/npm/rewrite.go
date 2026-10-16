package npm

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/samber/oops"
)

func rewriteMetadata(body []byte, proxyBase, registryBaseURL, metadataURL string) ([]byte, bool, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, oops.In("npm").Wrapf(err, "decode npm metadata")
	}
	versions, ok := doc["versions"].(map[string]any)
	if !ok {
		return body, false, nil
	}
	changed := false
	for _, versionValue := range versions {
		if rewriteVersionTarball(versionValue, proxyBase, registryBaseURL, metadataURL) {
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	rewritten, err := json.Marshal(doc)
	if err != nil {
		return nil, false, oops.In("npm").Wrapf(err, "encode npm metadata")
	}
	return rewritten, true, nil
}

func rewriteVersionTarball(versionValue any, proxyBase, registryBaseURL, metadataURL string) bool {
	version, ok := versionValue.(map[string]any)
	if !ok {
		return false
	}
	dist, ok := version["dist"].(map[string]any)
	if !ok {
		return false
	}
	tarball, ok := dist["tarball"].(string)
	if !ok || strings.TrimSpace(tarball) == "" {
		return false
	}
	local, ok := localTarballURL(tarball, proxyBase, registryBaseURL, metadataURL)
	if !ok {
		return false
	}
	dist["tarball"] = local
	return true
}

func localTarballURL(tarballURL, proxyBase, registryBaseURL, metadataURL string) (string, bool) {
	resolved, ok := resolveTarballURL(tarballURL, metadataURL)
	if !ok {
		return "", false
	}
	registryURL, ok := parseRegistryURL(registryBaseURL)
	if !ok {
		return "", false
	}
	tail, ok := registryTarballTail(resolved, registryURL)
	if !ok {
		return "", false
	}
	return formatLocalTarballURL(proxyBase, tail, resolved), true
}

func resolveTarballURL(tarballURL, metadataURL string) (*url.URL, bool) {
	parsed, err := url.Parse(tarballURL)
	if err != nil {
		return nil, false
	}
	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, false
	}
	baseURL, ok := parseRegistryURL(metadataURL)
	if !ok {
		return nil, false
	}
	return baseURL.ResolveReference(parsed), true
}

func parseRegistryURL(value string) (*url.URL, bool) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return nil, false
	}
	return parsed, parsed.Scheme == "http" || parsed.Scheme == "https"
}

func registryTarballTail(resolved, registryURL *url.URL) (string, bool) {
	registryBasePath := strings.TrimRight(registryURL.EscapedPath(), "/")
	tail := strings.TrimLeft(resolved.EscapedPath(), "/")
	if registryBasePath != "" && strings.HasPrefix(resolved.EscapedPath(), registryBasePath+"/") {
		tail = strings.TrimPrefix(resolved.EscapedPath(), registryBasePath+"/")
	}
	if tail == "" || !strings.Contains(tail, "/-/") || !strings.HasSuffix(strings.ToLower(tail), ".tgz") {
		return "", false
	}
	return tail, true
}

func formatLocalTarballURL(proxyBase, tail string, resolved *url.URL) string {
	base := strings.TrimRight(proxyBase, "/")
	local := base + "/" + tail
	if base == "" {
		local = "/npm/" + tail
	}
	if resolved.RawQuery != "" {
		local += "?" + resolved.RawQuery
	}
	if fragment := resolved.EscapedFragment(); fragment != "" {
		local += "#" + fragment
	}
	return local
}

func localBase(proxyBaseURL, alias string) string {
	proxyBaseURL = strings.TrimRight(strings.TrimSpace(proxyBaseURL), "/")
	if proxyBaseURL == "" {
		return "/npm/" + alias
	}
	return proxyBaseURL + "/npm/" + alias
}

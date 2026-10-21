package npm

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/samber/oops"
)

func rewriteMetadata(body []byte, proxyBase, registryBaseURL, metadataURL string) ([]byte, bool, error) {
	return rewriteMetadataWithLimit(body, proxyBase, registryBaseURL, metadataURL, npmMetadataMaxBytes)
}

func rewriteMetadataWithLimit(body []byte, proxyBase, registryBaseURL, metadataURL string, maxBytes int64) ([]byte, bool, error) {
	if err := checkNPMMetadataShape(body); err != nil {
		return nil, false, oops.In("npm").Wrapf(err, "validate npm metadata structure")
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, oops.In("npm").Wrapf(err, "decode npm metadata")
	}
	return rewriteMetadataDocument(body, doc, proxyBase, registryBaseURL, metadataURL, maxBytes)
}

func rewriteMetadataDocument(body []byte, doc map[string]any, proxyBase, registryBaseURL, metadataURL string, maxBytes int64) ([]byte, bool, error) {
	versions, ok := doc["versions"].(map[string]any)
	if !ok {
		return body, false, nil
	}
	changed := false
	growthBudget := maxBytes
	for _, versionValue := range versions {
		versionChanged, err := rewriteVersionTarball(versionValue, proxyBase, registryBaseURL, metadataURL, &growthBudget)
		if err != nil {
			return nil, false, err
		}
		if versionChanged {
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	outputSize, err := encodedJSONSize(doc)
	if err != nil {
		return nil, false, oops.In("npm").Wrapf(err, "measure rewritten npm metadata")
	}
	if outputSize > maxBytes {
		return nil, false, errors.New("rewritten npm metadata exceeds body limit")
	}
	rewritten, err := json.Marshal(doc)
	if err != nil {
		return nil, false, oops.In("npm").Wrapf(err, "encode npm metadata")
	}
	return rewritten, true, nil
}

func rewriteVersionTarball(
	versionValue any,
	proxyBase string,
	registryBaseURL string,
	metadataURL string,
	growthBudget *int64,
) (bool, error) {
	version, ok := versionValue.(map[string]any)
	if !ok {
		return false, nil
	}
	dist, ok := version["dist"].(map[string]any)
	if !ok {
		return false, nil
	}
	tarball, ok := dist["tarball"].(string)
	if !ok || strings.TrimSpace(tarball) == "" {
		return false, nil
	}
	localLength, ok := localTarballURLLength(tarball, proxyBase, registryBaseURL, metadataURL)
	if !ok {
		return false, nil
	}
	growth := localLength - int64(len(tarball))
	if growth > *growthBudget {
		return false, errors.New("rewritten npm metadata exceeds body limit")
	}
	local, ok := localTarballURL(tarball, proxyBase, registryBaseURL, metadataURL)
	if !ok {
		return false, nil
	}
	if growth > 0 {
		*growthBudget -= growth
	}
	dist["tarball"] = local
	return true, nil
}

func localTarballURL(tarballURL, proxyBase, registryBaseURL, metadataURL string) (string, bool) {
	resolved, tail, ok := localTarballTarget(tarballURL, registryBaseURL, metadataURL)
	if !ok {
		return "", false
	}
	return formatLocalTarballURL(proxyBase, tail, resolved), true
}

func localTarballURLLength(tarballURL, proxyBase, registryBaseURL, metadataURL string) (int64, bool) {
	resolved, tail, ok := localTarballTarget(tarballURL, registryBaseURL, metadataURL)
	if !ok {
		return 0, false
	}
	base := strings.TrimRight(proxyBase, "/")
	length := int64(len(base) + len(tail) + 1)
	if base == "" {
		length = int64(len("/npm/") + len(tail))
	}
	if resolved.RawQuery != "" {
		length += int64(len(resolved.RawQuery) + 1)
	}
	if fragment := resolved.EscapedFragment(); fragment != "" {
		length += int64(len(fragment) + 1)
	}
	return length, true
}

func localTarballTarget(tarballURL, registryBaseURL, metadataURL string) (*url.URL, string, bool) {
	resolved, ok := resolveTarballURL(tarballURL, metadataURL)
	if !ok {
		return nil, "", false
	}
	registryURL, ok := parseRegistryURL(registryBaseURL)
	if !ok {
		return nil, "", false
	}
	tail, ok := registryTarballTail(resolved, registryURL)
	if !ok {
		return nil, "", false
	}
	return resolved, tail, true
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

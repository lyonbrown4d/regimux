package npm

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/samber/oops"
)

func rewriteMetadata(body []byte, proxyBase string) ([]byte, bool, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, false, oops.In("npm-proxy").Wrapf(err, "decode npm metadata")
	}
	versions, ok := doc["versions"].(map[string]any)
	if !ok {
		return body, false, nil
	}
	changed := false
	for _, versionValue := range versions {
		if rewriteVersionTarball(versionValue, proxyBase) {
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	rewritten, err := json.Marshal(doc)
	if err != nil {
		return nil, false, oops.In("npm-proxy").Wrapf(err, "encode npm metadata")
	}
	return rewritten, true, nil
}

func rewriteVersionTarball(versionValue any, proxyBase string) bool {
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
	local, ok := localTarballURL(tarball, proxyBase)
	if !ok {
		return false
	}
	dist["tarball"] = local
	return true
}

func localTarballURL(tarballURL, proxyBase string) (string, bool) {
	parsed, err := url.Parse(tarballURL)
	if err != nil || parsed.Path == "" {
		return "", false
	}
	tail := strings.TrimLeft(parsed.EscapedPath(), "/")
	if tail == "" || !strings.Contains(tail, "/-/") || !strings.HasSuffix(strings.ToLower(tail), ".tgz") {
		return "", false
	}
	base := strings.TrimRight(proxyBase, "/")
	if base == "" {
		return "/npm/" + tail, true
	}
	return base + "/" + tail, true
}

func localBase(proxyBaseURL, alias string) string {
	proxyBaseURL = strings.TrimRight(strings.TrimSpace(proxyBaseURL), "/")
	if proxyBaseURL == "" {
		return "/npm/" + alias
	}
	return proxyBaseURL + "/npm/" + alias
}

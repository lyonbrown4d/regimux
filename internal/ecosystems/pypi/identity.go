package pypi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path"
	"strings"
)

const (
	packageProjectQuery = "__regimux_project"
	packageHashQuery    = "__regimux_sha256"
)

func packageIdentity(req Request, route Route) (Request, Route) {
	query, err := url.ParseQuery(route.Query)
	if err != nil {
		return req, route
	}
	if project := query.Get(packageProjectQuery); project != "" {
		req.Project = project
	}
	if hash := query.Get(packageHashQuery); hash != "" {
		req.SHA256 = hash
	}
	route.Query = stripPyPIProxyQuery(route.Query)
	if route.Query != "" {
		digest := sha256.Sum256([]byte(route.Query))
		route.Reference += "?query-sha256=" + hex.EncodeToString(digest[:])
	}
	return req, route
}

func addPackageIdentity(href string, simpleURL *url.URL, fragment string) string {
	hash := strings.TrimPrefix(fragment, "sha256=")
	if !strings.HasPrefix(fragment, "sha256=") || !validSHA256(hash) {
		return href
	}
	project, err := NormalizeProjectName(path.Base(strings.TrimRight(simpleURL.Path, "/")))
	if err != nil {
		return href
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return href
	}
	q := url.Values{}
	q.Set(packageProjectQuery, project)
	q.Set(packageHashQuery, strings.ToLower(hash))
	if parsed.RawQuery != "" {
		parsed.RawQuery += "&"
	}
	parsed.RawQuery += q.Encode()
	return parsed.String()
}

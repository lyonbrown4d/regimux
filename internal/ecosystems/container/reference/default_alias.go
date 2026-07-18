package reference

import "strings"

type AliasLookup func(alias string) bool

func ParseWithDefaultAlias(path, defaultAlias string, hasAlias AliasLookup) (Route, error) {
	defaultAlias = strings.Trim(strings.TrimSpace(defaultAlias), "/")
	trimmedPath := strings.Trim(strings.TrimSpace(path), "/")
	if defaultAlias == "" || trimmedPath == "v2" {
		return Parse(path)
	}

	segments := strings.Split(trimmedPath, "/")
	if len(segments) < 2 || segments[0] != "v2" || segments[1] == "" {
		return Parse(path)
	}

	requestedAlias := segments[1]
	if requestedAlias == defaultAlias || hasAlias != nil && hasAlias(requestedAlias) {
		return Parse(path)
	}

	rewritten := make([]string, 0, len(segments)+1)
	rewritten = append(rewritten, "v2", defaultAlias)
	rewritten = append(rewritten, segments[1:]...)
	return Parse("/" + strings.Join(rewritten, "/"))
}

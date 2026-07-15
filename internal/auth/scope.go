package auth

import (
	"regexp"
	"strings"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/samber/lo"
)

type Scope struct {
	Type    string
	Name    string
	Actions []string
}

func (s Scope) RequiresPull() bool {
	if s.Type != ScopeTypeRepository {
		return false
	}
	return lo.ContainsBy(s.Actions, func(action string) bool {
		return strings.TrimSpace(action) == ActionPull
	})
}

func parseScope(value string) (Scope, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return Scope{}, newAuthError(authx.ErrorCodeInvalidAuthorizationModel, "registry token scope is invalid")
	}
	scope := Scope{
		Type: strings.TrimSpace(parts[0]),
		Name: strings.Trim(strings.TrimSpace(parts[1]), "/"),
	}
	scope.Actions = lo.FilterMap(strings.Split(parts[2], ","), func(action string, _ int) (string, bool) {
		action = strings.TrimSpace(action)
		return action, action != ""
	})
	if len(scope.Actions) == 0 {
		return Scope{}, newAuthError(authx.ErrorCodeInvalidAuthorizationModel, "registry token scope is incomplete")
	}
	if scope.Type == "" || scope.Name == "" {
		return Scope{}, newAuthError(authx.ErrorCodeInvalidAuthorizationModel, "registry token scope is incomplete")
	}
	return scope, nil
}

func normalizeScopes(values []string) []string {
	return lo.Uniq(lo.FilterMap(values, func(value string, _ int) (string, bool) {
		value = strings.TrimSpace(value)
		return value, value != ""
	}))
}

func repositoryPatternMatches(pattern, resource string) bool {
	pattern = strings.Trim(strings.TrimSpace(pattern), "/")
	resource = strings.Trim(strings.TrimSpace(resource), "/")
	if pattern == "" || resource == "" {
		return false
	}
	if pattern == "*" || pattern == resource {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		return resource == prefix || strings.HasPrefix(resource, prefix+"/")
	}
	if !strings.Contains(pattern, "*") {
		return false
	}
	expr := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
	matched, err := regexp.MatchString(expr, resource)
	return err == nil && matched
}

func principalHasPullScope(principal authx.Principal, resource string) bool {
	return listContains(principal.Permissions, ScopeTypeRepository+":"+strings.Trim(resource, "/")+":"+ActionPull)
}

func listContains(values *collectionlist.List[string], candidate string) bool {
	if values == nil || candidate == "" {
		return false
	}
	return values.AnyMatch(func(_ int, value string) bool {
		return strings.TrimSpace(value) == candidate
	})
}

func isRegistryPingPath(path string) bool {
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	return path == "/v2"
}

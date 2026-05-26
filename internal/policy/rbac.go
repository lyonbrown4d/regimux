// Package policy contains access policy primitives.
package policy

import "strings"

type Rule struct {
	Subject string
	Allow   []string
}

func Match(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if before, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(value, before)
	}
	return false
}

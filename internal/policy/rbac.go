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
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return false
}


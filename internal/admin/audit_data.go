package admin

import (
	"sort"
	"strings"

	"github.com/lyonbrown4d/regimux/internal/config"
)

func auditSummary(cfg config.Config) AuditSummary {
	return AuditSummary{
		AuthEnabled:        cfg.Auth.Enabled,
		UserCount:          len(cfg.Auth.Users),
		Users:              auditUserRows(cfg.Auth.Users),
		RecentLogins:       nil,
		LoginDataAvailable: false,
	}
}

func auditUserRows(users map[string]config.AuthUserConfig) []AuditUserRow {
	usernames := make([]string, 0, len(users))
	for username := range users {
		usernames = append(usernames, username)
	}
	sort.Strings(usernames)

	rows := make([]AuditUserRow, 0, len(usernames))
	for _, username := range usernames {
		user := users[username]
		rows = append(rows, AuditUserRow{
			Username:         username,
			RepositoryScopes: listString(user.Repositories),
			Groups:           listString(user.Groups),
			Credential:       credentialKind(user),
		})
	}
	return rows
}

func listString(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	clean := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			clean = append(clean, value)
		}
	}
	if len(clean) == 0 {
		return "-"
	}
	return strings.Join(clean, ", ")
}

func credentialKind(user config.AuthUserConfig) string {
	hasPassword := strings.TrimSpace(user.Password) != ""
	hasHash := strings.TrimSpace(user.PasswordHash) != ""
	switch {
	case hasPassword && hasHash:
		return "password, password_hash"
	case hasHash:
		return "password_hash"
	case hasPassword:
		return "password"
	default:
		return "-"
	}
}

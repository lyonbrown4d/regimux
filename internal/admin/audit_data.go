package admin

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
)

func auditSummary(cfg config.Config) AuditSummary {
	users := collectionmapping.NewMapFrom(cfg.Auth.Users)
	return AuditSummary{
		AuthEnabled:        cfg.Auth.Enabled,
		UserCount:          len(cfg.Auth.Users),
		Users:              auditUserRows(users).Values(),
		RecentLogins:       nil,
		LoginDataAvailable: false,
	}
}

func auditUserRows(users *collectionmapping.Map[string, config.AuthUserConfig]) *collectionlist.List[AuditUserRow] {
	if users == nil {
		return collectionlist.NewList[AuditUserRow]()
	}
	usernames := collectionlist.NewList(users.Keys()...).
		Sort(strings.Compare)

	rows := collectionlist.NewListWithCapacity[AuditUserRow](usernames.Len())
	usernames.Range(func(_ int, username string) bool {
		user, _ := users.Get(username)
		rows.Add(AuditUserRow{
			Username:         username,
			RepositoryScopes: listString(user.Repositories),
			Groups:           listString(user.Groups),
			Credential:       credentialKind(user),
		})
		return true
	})
	return rows
}

func listString(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	clean := collectionlist.FilterMapList(collectionlist.NewList(values...), func(_ int, value string) (string, bool) {
		value = strings.TrimSpace(value)
		return value, value != ""
	})
	if clean.Len() == 0 {
		return "-"
	}
	return clean.Join(", ")
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

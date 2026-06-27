package admin

import (
	"strings"

	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
	"github.com/samber/lo"
)

func auditSummary(cfg config.Config) AuditSummary {
	users := collectionmapping.NewMapFrom(cfg.Auth.Users)
	return AuditSummary{
		AuthEnabled:        cfg.Auth.Enabled,
		UserCount:          len(cfg.Auth.Users),
		Users:              auditUserRows(users),
		RecentLogins:       collectionlist.NewList[AuditLoginRow](),
		LoginDataAvailable: false,
	}
}

func auditUserRows(users *collectionmapping.Map[string, config.AuthUserConfig]) *collectionlist.List[AuditUserRow] {
	if users == nil {
		return collectionlist.NewList[AuditUserRow]()
	}
	usernames := collectionlist.NewList(users.Keys()...).Sort(strings.Compare)

	return collectionlist.MapList(usernames, func(_ int, username string) AuditUserRow {
		user, _ := users.Get(username)
		return AuditUserRow{
			Username:         username,
			RepositoryScopes: listString(user.Repositories),
			Groups:           listString(user.Groups),
			Credential:       credentialKind(user),
		}
	})
}

func listString(values []string) string {
	clean := lo.FilterMap(values, func(value string, _ int) (string, bool) {
		value = strings.TrimSpace(value)
		return value, value != ""
	})
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

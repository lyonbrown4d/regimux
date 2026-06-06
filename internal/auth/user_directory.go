package auth

import (
	"context"
	"strings"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	collectionmapping "github.com/arcgolabs/collectionx/mapping"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type UserDirectory struct {
	users *collectionmapping.Map[string, userRecord]
}

type userRecord struct {
	config       config.AuthUserConfig
	groups       *collectionlist.List[string]
	repositories *collectionlist.List[string]
}

func NewUserDirectory(auth config.RegistryAuthConfig) *UserDirectory {
	records := collectionmapping.NewMapWithCapacity[string, userRecord](len(auth.Users))
	for username, user := range auth.Users {
		records.Set(username, userRecord{
			config:       user,
			groups:       collectionlist.NewListWithCapacity[string](len(user.Groups), user.Groups...),
			repositories: collectionlist.NewListWithCapacity[string](len(user.Repositories), user.Repositories...),
		})
	}
	return &UserDirectory{users: records}
}

func (d *UserDirectory) Len() int {
	if d == nil || d.users == nil {
		return 0
	}
	return d.users.Len()
}

func (d *UserDirectory) Authenticate(_ context.Context, credential BasicCredential) (authx.AuthenticationResult, error) {
	username := strings.TrimSpace(credential.Username)
	if username == "" || credential.Password == "" {
		return authx.AuthenticationResult{}, newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are required")
	}
	record, ok := d.user(username)
	if !ok {
		return authx.AuthenticationResult{}, newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are invalid")
	}
	if err := verifyPassword(record.config, credential.Password); err != nil {
		return authx.AuthenticationResult{}, err
	}
	return authx.AuthenticationResult{
		Principal: authx.Principal{
			ID:          username,
			Roles:       cloneStringList(record.groups),
			Permissions: repositoryPermissions(record.repositories),
		},
	}, nil
}

func (d *UserDirectory) Allows(username, resource string) bool {
	record, ok := d.user(username)
	if !ok || record.repositories == nil {
		return false
	}
	return record.repositories.AnyMatch(func(_ int, pattern string) bool {
		return repositoryPatternMatches(pattern, resource)
	})
}

func (d *UserDirectory) user(username string) (userRecord, bool) {
	if d == nil || d.users == nil {
		return userRecord{}, false
	}
	return d.users.Get(username)
}

func repositoryPermissions(repositories *collectionlist.List[string]) *collectionlist.List[string] {
	return cloneStringList(repositories)
}

func cloneStringList(values *collectionlist.List[string]) *collectionlist.List[string] {
	if values == nil {
		return collectionlist.NewList[string]()
	}
	return values.Clone()
}

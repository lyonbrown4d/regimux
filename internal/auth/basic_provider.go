package auth

import (
	"context"
	"strings"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type BasicProvider struct {
	auth config.RegistryAuthConfig
}

type BasicAuthenticationProvider struct {
	authx.AuthenticationProvider
}

func NewBasicProvider(auth config.RegistryAuthConfig) *BasicProvider {
	return &BasicProvider{auth: auth}
}

func NewBasicAuthenticationProvider(auth config.RegistryAuthConfig) *BasicAuthenticationProvider {
	return &BasicAuthenticationProvider{
		AuthenticationProvider: authx.NewAuthenticationProvider[BasicCredential](NewBasicProvider(auth)),
	}
}

func (p *BasicProvider) Authenticate(_ context.Context, credential BasicCredential) (authx.AuthenticationResult, error) {
	username := strings.TrimSpace(credential.Username)
	if username == "" || credential.Password == "" {
		return authx.AuthenticationResult{}, newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are required")
	}
	user, ok := p.auth.Users[username]
	if !ok {
		return authx.AuthenticationResult{}, newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are invalid")
	}
	if err := verifyPassword(user, credential.Password); err != nil {
		return authx.AuthenticationResult{}, err
	}
	return authx.AuthenticationResult{
		Principal: authx.Principal{
			ID:          username,
			Roles:       collectionlist.NewListWithCapacity[string](len(user.Groups), user.Groups...),
			Permissions: collectionlist.NewListWithCapacity[string](len(user.Repositories), user.Repositories...),
		},
	}, nil
}

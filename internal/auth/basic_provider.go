package auth

import (
	"context"

	"github.com/arcgolabs/authx"
)

type BasicProvider struct {
	users *UserDirectory
}

type BasicAuthenticationProvider struct {
	authx.AuthenticationProvider
}

func NewBasicProvider(users *UserDirectory) *BasicProvider {
	return &BasicProvider{users: users}
}

func NewBasicAuthenticationProvider(users *UserDirectory) *BasicAuthenticationProvider {
	return &BasicAuthenticationProvider{
		AuthenticationProvider: authx.NewAuthenticationProvider[BasicCredential](NewBasicProvider(users)),
	}
}

func (p *BasicProvider) Authenticate(ctx context.Context, credential BasicCredential) (authx.AuthenticationResult, error) {
	if p == nil || p.users == nil {
		return authx.AuthenticationResult{}, newAuthError(authx.ErrorCodeUnauthenticated, "registry credentials are invalid")
	}
	return p.users.Authenticate(ctx, credential)
}

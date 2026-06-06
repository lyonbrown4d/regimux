package auth

import (
	"strings"
	"time"

	"github.com/arcgolabs/authx"
	authjwt "github.com/arcgolabs/authx/jwt"
	"github.com/lyonbrown4d/regimux/internal/config"
)

type JWTAuthenticationProvider struct {
	authx.AuthenticationProvider
}

func NewJWTAuthenticationProvider(auth config.RegistryAuthConfig) *JWTAuthenticationProvider {
	return &JWTAuthenticationProvider{
		AuthenticationProvider: authjwt.NewAuthenticationProvider(
			authjwt.WithHMACSecret([]byte(strings.TrimSpace(auth.TokenSecret))),
			authjwt.WithIssuer(registryAuthIssuer(auth)),
			authjwt.WithAudience(registryAuthServiceName(auth)),
			authjwt.WithRequiredExpiration(),
			authjwt.WithRequiredIssuedAt(),
			authjwt.WithClockSkew(time.Minute),
		),
	}
}

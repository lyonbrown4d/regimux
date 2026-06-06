package auth

import (
	"log/slog"

	"github.com/arcgolabs/authx"
	collectionlist "github.com/arcgolabs/collectionx/list"
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/config"
)

var Module = dix.NewModule("auth",
	dix.Providers(
		dix.Provider1[config.RegistryAuthConfig, config.Config](func(cfg config.Config) config.RegistryAuthConfig {
			return cfg.Auth
		}),
		dix.Provider1[*BasicAuthenticationProvider, config.RegistryAuthConfig](
			NewBasicAuthenticationProvider,
			dix.Into[authx.AuthenticationProvider](dix.Key("auth.basic"), dix.Order(0)),
		),
		dix.Provider1[*JWTAuthenticationProvider, config.RegistryAuthConfig](
			NewJWTAuthenticationProvider,
			dix.Into[authx.AuthenticationProvider](dix.Key("auth.jwt"), dix.Order(10)),
		),
		dix.ProviderErr4[*Service, config.Config, *slog.Logger, *collectionlist.List[authx.AuthenticationProvider], *collectionlist.List[ResourceResolver]](NewService),
	),
)

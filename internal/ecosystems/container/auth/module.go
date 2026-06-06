// Package containerauth provides container-specific auth resolvers.
package containerauth

import (
	"github.com/arcgolabs/dix"
	authcore "github.com/lyonbrown4d/regimux/internal/auth"
	"github.com/lyonbrown4d/regimux/internal/config"
)

var Module = dix.NewModule("container-auth",
	dix.Providers(
		dix.Provider1[authcore.ResourceResolver, config.Config](NewResourceResolver, dix.Into[authcore.ResourceResolver](dix.Order(0))),
	),
)

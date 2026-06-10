package registrytool

import (
	"github.com/arcgolabs/dix"
	"github.com/lyonbrown4d/regimux/internal/clientfactory"
)

var Module = dix.NewModule("container-registrytool",
	dix.Providers(
		dix.Provider1[*Client, *clientfactory.Factory](func(factory *clientfactory.Factory) *Client {
			return NewClient(Dependencies{Factory: factory})
		}),
	),
)

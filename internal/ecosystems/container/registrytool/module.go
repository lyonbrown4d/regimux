package registrytool

import "github.com/arcgolabs/dix"

var Module = dix.NewModule("container-registrytool",
	dix.Providers(
		dix.Provider0[*Client](NewClient),
	),
)

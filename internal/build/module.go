package build

import "github.com/arcgolabs/dix"

type Version string

func Module(version string) dix.Module {
	return dix.NewModule("build",
		dix.Providers(
			dix.Provider0[Version](func() Version {
				return Version(version)
			}),
		),
	)
}

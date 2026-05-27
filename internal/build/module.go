package build

import (
	"runtime/debug"
	"strings"

	"github.com/arcgolabs/dix"
)

type Version string

const fallbackVersion = "dev"

var Module = dix.NewModule("build",
	dix.Providers(
		dix.Provider0[Version](func() Version {
			return Version(VersionFromBuildInfo())
		}),
	),
)

func VersionFromBuildInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallbackVersion
	}

	if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
		return version
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			if revision := strings.TrimSpace(setting.Value); revision != "" {
				return revision
			}
		}
	}

	return fallbackVersion
}

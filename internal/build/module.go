// Package build exposes build metadata providers.
package build

import (
	"runtime/debug"
	"strings"

	"github.com/arcgolabs/dix"
	"github.com/samber/lo"
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

	if setting, ok := lo.Find(info.Settings, func(setting debug.BuildSetting) bool {
		return setting.Key == "vcs.revision" && strings.TrimSpace(setting.Value) != ""
	}); ok {
		return strings.TrimSpace(setting.Value)
	}

	return fallbackVersion
}

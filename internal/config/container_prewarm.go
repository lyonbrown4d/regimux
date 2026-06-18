package config

import (
	"runtime"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/containerd/platforms"
	"github.com/samber/oops"
)

const ContainerPrewarmAllPlatforms = "all"

var activeContainerPrewarmPlatforms atomic.Value

func normalizeContainerPrewarmConfig(cfg ContainerPrewarmConfig) (ContainerPrewarmConfig, error) {
	platformValues, err := normalizeContainerPrewarmPlatforms(cfg.Platforms)
	if err != nil {
		return ContainerPrewarmConfig{}, err
	}
	cfg.Platforms = platformValues
	return cfg, nil
}

func normalizeContainerPrewarmPlatforms(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{DefaultContainerPrewarmPlatform()}, nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		platform, err := normalizeContainerPrewarmPlatform(value)
		if err != nil {
			return nil, err
		}
		if platform == "" {
			continue
		}
		out = append(out, platform)
	}
	out = uniqueStrings(out)
	if len(out) == 0 {
		return []string{DefaultContainerPrewarmPlatform()}, nil
	}
	if slices.Contains(out, ContainerPrewarmAllPlatforms) && len(out) > 1 {
		return nil, oops.In("config").
			With("platforms", out).
			Errorf("container.prewarm.platforms must use all by itself")
	}
	return out, nil
}

func normalizeContainerPrewarmPlatform(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if value == ContainerPrewarmAllPlatforms {
		return ContainerPrewarmAllPlatforms, nil
	}
	if !strings.Contains(value, "/") {
		return "", invalidContainerPrewarmPlatformError(value)
	}
	platform, err := platforms.Parse(value)
	if err != nil {
		return "", oops.In("config").With("platform", value).Wrapf(err, "parse container.prewarm.platforms")
	}
	platform = platforms.Normalize(platform)
	if platform.OS == "" || platform.Architecture == "" {
		return "", invalidContainerPrewarmPlatformError(value)
	}
	return platforms.Format(platform), nil
}

func invalidContainerPrewarmPlatformError(value string) error {
	return oops.In("config").
		With("platform", value).
		Errorf("container.prewarm.platforms entries must be all or os/arch[/variant]")
}

func DefaultContainerPrewarmPlatform() string {
	return defaultContainerPrewarmPlatform(runtime.GOARCH)
}

func defaultContainerPrewarmPlatform(arch string) string {
	arch = strings.ToLower(strings.TrimSpace(arch))
	if arch == "" {
		arch = runtime.GOARCH
	}
	platform, err := platforms.Parse("linux/" + arch)
	if err != nil {
		return "linux/" + arch
	}
	return platforms.Format(platforms.Normalize(platform))
}

func activateContainerPrewarmPlatforms(container ContainerConfig) {
	platformsByAlias := make(map[string][]string, len(container))
	for key := range container {
		alias := strings.TrimSpace(key)
		if alias == "" {
			continue
		}
		platformsByAlias[alias] = append([]string(nil), container[key].Prewarm.Platforms...)
	}
	activeContainerPrewarmPlatforms.Store(platformsByAlias)
}

func ActiveContainerPrewarmPlatforms(alias string) []string {
	platformsByAlias, ok := activeContainerPrewarmPlatforms.Load().(map[string][]string)
	if !ok {
		return []string{DefaultContainerPrewarmPlatform()}
	}
	if len(platformsByAlias) > 0 {
		if values, ok := platformsByAlias[strings.TrimSpace(alias)]; ok && len(values) > 0 {
			return append([]string(nil), values...)
		}
	}
	return []string{DefaultContainerPrewarmPlatform()}
}
